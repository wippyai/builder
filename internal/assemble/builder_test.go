// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture() Manifest {
	return Manifest{Schema: 1, Name: "hello", Runtime: Runtime{Repository: "https://github.com/wippyai/runtime.git", Commit: strings.Repeat("a", 40), Go: "1.27.0", Tags: []string{}}, Application: Application{Module: "example/hello", Command: "hello", Mode: "base", Packs: []Pack{{Module: "example/hello", Version: "1.0.0", Path: "hello.wapp", SHA256: strings.Repeat("a", 64)}}}}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestManifest(t *testing.T) {
	changes := []func(*Manifest){func(m *Manifest) { m.Runtime.Commit = "main" }, func(m *Manifest) { m.Runtime.Go = "latest" }, func(m *Manifest) { m.Application.Mode = "overlay" }, func(m *Manifest) { m.Application.Module = "example/missing" }, func(m *Manifest) { m.Application.Packs = append(m.Application.Packs, m.Application.Packs[0]) }, func(m *Manifest) { m.Application.Packs[0].Version = "latest" }, func(m *Manifest) { m.Application.Packs[0].Path = "../escape" }, func(m *Manifest) { m.Application.DataEnv = map[string]string{"DB": "../escape"} }, func(m *Manifest) {
		m.Native = []Native{{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/watch", Factory: `Component();panic("x")`}}
	}}
	m := fixture()
	must(t, m.Validate())
	for i, change := range changes {
		m := fixture()
		change(&m)
		if m.Validate() == nil {
			t.Errorf("mutation %d accepted", i)
		}
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	must(t, WriteJSON(path, fixture()))
	_, err := ReadManifest(path)
	must(t, err)
	data, err := os.ReadFile(path)
	must(t, err)
	data = bytes.Replace(data, []byte(`"schema": 1`), []byte(`"schema": 1, "unknown": true`), 1)
	must(t, os.WriteFile(path, data, 0600))
	if _, err = ReadManifest(path); err == nil {
		t.Fatal("unknown manifest key accepted")
	}
}
func TestGeneratedSource(t *testing.T) {
	m := fixture()
	m.Application.Command = "hello\"; panic(\"injected\") //"
	m.Native = []Native{{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/watch", Factory: "Component"}}
	for _, toolchain := range []bool{false, true} {
		source, err := Generate(&m, toolchain)
		must(t, err)
		_, err = parser.ParseFile(token.NewFileSet(), "main.go", source, parser.AllErrors)
		must(t, err)
		if !bytes.Contains(source, []byte("native0.Component()")) {
			t.Fatal("missing native registration")
		}
		if !toolchain && !bytes.Contains(source, []byte(`Command: "hello\"; panic(\"injected\") //"`)) {
			t.Fatalf("command was not escaped:\n%s", source)
		}
	}
}
func TestChecksumBeforeTools(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	must(t, WriteJSON(path, fixture()))
	must(t, os.WriteFile(filepath.Join(root, "hello.wapp"), []byte("wrong"), 0600))
	t.Setenv("PATH", "/nonexistent")
	err := Build(path, filepath.Join(root, "binary"), false)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum preflight, got %v", err)
	}
}
func TestArchiveDeterminismAndTampering(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "hello")
	must(t, os.WriteFile(binary, []byte("binary"), 0755))
	sum, err := Digest(binary)
	must(t, err)
	must(t, WriteJSON(binary+".provenance.json", Provenance{BinarySHA256: sum}))
	for _, suffix := range []string{".LICENSES.txt", ".go.mod", ".go.sum", ".runtime-patches.tar.gz"} {
		must(t, os.WriteFile(binary+suffix, []byte("sidecar"), 0644))
	}
	first, second := filepath.Join(root, "first.tar.gz"), filepath.Join(root, "second.tar.gz")
	must(t, Package(binary, first))
	must(t, os.Chtimes(binary, time.Now(), time.Now()))
	must(t, Package(binary, second))
	a, err := os.ReadFile(first)
	must(t, err)
	b, err := os.ReadFile(second)
	must(t, err)
	if !bytes.Equal(a, b) {
		t.Fatal("archive depends on host metadata")
	}
	must(t, os.WriteFile(binary, []byte("tampered"), 0755))
	if err = Package(binary, second); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("tampered binary accepted: %v", err)
	}
}
func TestSealAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	must(t, WriteJSON(path, fixture()))
	must(t, os.WriteFile(filepath.Join(root, "hello.wapp"), []byte("pack"), 0600))
	must(t, Seal(path, "v2.0.0", "bootstrap"))
	m, err := ReadManifest(path)
	must(t, err)
	sum, err := Digest(filepath.Join(root, "hello.wapp"))
	must(t, err)
	if m.Application.Mode != "bootstrap" || m.Application.Packs[0].Version != "2.0.0" || m.Application.Packs[0].SHA256 != sum {
		t.Fatal("seal did not preserve selected mode/version/hash")
	}
}

func TestStandalone(t *testing.T) {
	binary := os.Getenv("WIPPY_TEST_BINARY")
	if binary == "" {
		t.Skip("requires assembled hello binary")
	}
	binary, err := filepath.Abs(binary)
	must(t, err)
	root := t.TempDir()
	cwd := filepath.Join(root, "empty directory")
	must(t, os.Mkdir(cwd, 0700))
	state := filepath.Join(root, "application state")
	env := os.Environ()
	for k, v := range map[string]string{"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"), "PATH": "/nonexistent"} {
		env = setEnv(env, k, v)
	}
	invoke := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, binary, append([]string{"--state-dir", state}, args...)...)
		c.Dir = cwd
		c.Env = env
		data, err := c.CombinedOutput()
		return string(data), err
	}
	for _, name := range []string{"Ada", "Again"} {
		output, err := invoke("run", name)
		if err != nil || !strings.Contains(output, "Hello, "+name+"!") {
			t.Fatalf("standalone boot: %v\n%s", err, output)
		}
	}
	output, err := invoke("--base", "run", "Recovery")
	if os.Getenv("WIPPY_TEST_BOOTSTRAP") == "1" {
		if err == nil || !strings.Contains(output, "bootstrap applications do not expose a base deployment") {
			t.Fatalf("bootstrap base rejection: %v\n%s", err, output)
		}
	} else if err != nil || !strings.Contains(output, "Hello, Recovery!") {
		t.Fatalf("base recovery: %v\n%s", err, output)
	}
	files, err := os.ReadDir(cwd)
	must(t, err)
	if len(files) != 0 {
		t.Fatal("state written into caller directory")
	}
	_, err = os.Stat(filepath.Join(state, "deployment", "wippy.lock"))
	must(t, err)
}
func TestHub(t *testing.T) {
	binary, path := os.Getenv("WIPPY_TEST_BINARY"), os.Getenv("WIPPY_TEST_MANIFEST")
	if binary == "" || path == "" {
		t.Skip("requires assembled hello binary and manifest")
	}
	binary, err := filepath.Abs(binary)
	must(t, err)
	path, err = filepath.Abs(path)
	must(t, err)
	m, err := ReadManifest(path)
	must(t, err)
	root := t.TempDir()
	source := filepath.Join(root, "runtime")
	must(t, run("", nil, "git", "clone", "--no-checkout", "--filter=blob:none", m.Runtime.Repository, source))
	must(t, run(source, nil, "git", "checkout", "--detach", m.Runtime.Commit))
	env := os.Environ()
	for k, v := range map[string]string{"GOWORK": "off", "GOTOOLCHAIN": "go" + m.Runtime.Go, "WIPPY_TEST_APPLICATION_BINARY": binary, "WIPPY_TEST_APPLICATION_PACK": filepath.Join(filepath.Dir(path), m.Application.Packs[0].Path)} {
		env = setEnv(env, k, v)
	}
	// Require the acceptance test to exist so a renamed or removed test cannot
	// silently turn this CI gate into a successful no-op.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "go", "test", "-json", "-tags", strings.Join(m.Runtime.Tags, ","), "./application", "-run", "^TestHubBinaryUpdate$", "-count=1")
	c.Dir = source
	c.Env = env
	var stderr bytes.Buffer
	c.Stderr = &stderr
	data, err := c.Output()
	if err != nil {
		t.Fatalf("Hub acceptance: %v\n%s\n%s", err, data, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	passed := false
	for decoder.More() {
		var event struct{ Action, Test, Output string }
		must(t, decoder.Decode(&event))
		if event.Action == "pass" && event.Test == "TestHubBinaryUpdate" {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("Hub binary acceptance did not run:\n%s", data)
	}
}
