// SPDX-License-Identifier: MIT
package assemble

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixture() Manifest {
	return Manifest{Schema: 1, Name: "hello", Runtime: Runtime{Repository: "https://github.com/wippyai/runtime.git", Commit: strings.Repeat("a", 40), Go: "1.27.0", Tags: []string{}}, Application: Application{Module: "example/hello", Command: "hello", Packs: []Pack{{Module: "example/hello", Version: "1.0.0", Path: "hello.wapp", SHA256: strings.Repeat("a", 64)}}}}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestManifest(t *testing.T) {
	changes := []func(*Manifest){func(m *Manifest) { m.Runtime.Commit = "main" }, func(m *Manifest) { m.Runtime.Go = "latest" }, func(m *Manifest) { m.Application.Module = "example/missing" }, func(m *Manifest) { m.Application.Packs = append(m.Application.Packs, m.Application.Packs[0]) }, func(m *Manifest) { m.Application.Packs[0].Version = "latest" }, func(m *Manifest) { m.Application.Packs[0].Path = "../escape" }, func(m *Manifest) { m.Application.Data = map[string]string{"DB": "../escape"} }, func(m *Manifest) {
		m.Native = []Native{{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/watch", Factory: `Component();panic("x")`}}
	}, func(m *Manifest) {
		m.Native = []Native{{Module: "example.com/one", Version: "v1.0.0", Package: "example.com/one", Factory: "Component", Host: true}, {Module: "example.com/two", Version: "v1.0.0", Package: "example.com/two", Factory: "Component", Host: true}}
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

func TestNativeComponentsShareModuleVersion(t *testing.T) {
	m := fixture()
	m.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/desktop", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/docker", Factory: "Docker"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/shared", Factory: "First"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/shared", Factory: "Second"},
	}
	must(t, m.Validate())

	m.Native[3].Version = "v1.1.0"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "conflicting versions") {
		t.Fatalf("accepted components with conflicting module versions: %v", err)
	}
	m.Native[3].Version = "v1.0.0"
	m.Native[3].Factory = "First"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate native component") {
		t.Fatalf("accepted duplicate package and factory: %v", err)
	}
}

func TestPrepareDependenciesDeduplicatesModuleRequirements(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "calls")
	fakeGo := filepath.Join(root, "go")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$WIPPY_TEST_GO_CALLS"
if [ "$1" = list ]; then
  for argument do package="$argument"; done
  printf '{"ImportPath":%s,"Module":{"Path":"example.com/native","Version":"v1.0.0"}}\n' "\"$package\""
fi
`
	must(t, os.WriteFile(fakeGo, []byte(script), 0700))
	t.Setenv("PATH", root)
	env := setEnv(os.Environ(), "WIPPY_TEST_GO_CALLS", log)
	m := fixture()
	m.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/desktop", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/docker", Factory: "Docker"},
	}
	must(t, prepareDependencies(root, env, &m))
	calls, err := os.ReadFile(log)
	must(t, err)
	if strings.Count(string(calls), "mod edit -require=example.com/native@v1.0.0") != 1 {
		t.Fatalf("module requirement was not deduplicated:\n%s", calls)
	}
	if strings.Count(string(calls), "list -mod=readonly") != 2 {
		t.Fatalf("selected packages were not each verified:\n%s", calls)
	}
}
func TestGeneratedSource(t *testing.T) {
	m := fixture()
	m.Application.Command = "hello\"; panic(\"injected\") //"
	m.Native = []Native{{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/watch", Factory: "Component", Host: true}}
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
		if !toolchain && !bytes.Contains(source, []byte("Host: component0")) {
			t.Fatalf("host factory result was not reused:\n%s", source)
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
	artifacts := artifactsFor(binary)
	m := fixture()
	for _, file := range artifacts.recorded() {
		must(t, os.WriteFile(file.Path, []byte("content"), 0644))
	}
	must(t, os.Chmod(binary, 0755))
	hashes, err := artifacts.hashes()
	must(t, err)
	provenance := Provenance{Schema: 1, Mode: "application", Manifest: &m, Artifacts: hashes}
	must(t, WriteJSON(artifacts.Provenance.Path, provenance))
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
	compressed, err := gzip.NewReader(bytes.NewReader(a))
	must(t, err)
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	var names []string
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		must(t, err)
		names = append(names, header.Name)
	}
	want := []string{"hello", "hello.LICENSES.txt", "hello.go.mod", "hello.go.sum", "hello.provenance.json", "hello.runtime-patches.tar.gz"}
	if !slices.Equal(names, want) {
		t.Fatalf("unexpected release archive contents: %v", names)
	}
	if err = Package(binary, binary); err == nil {
		t.Fatal("archive overwrote its binary input")
	}
	for _, file := range artifacts.recorded() {
		must(t, os.WriteFile(file.Path, []byte("tampered"), 0644))
		if err = Package(binary, second); err == nil {
			t.Fatalf("tampered %s accepted", file.Name)
		}
		must(t, os.WriteFile(file.Path, []byte("content"), 0644))
	}
	must(t, os.WriteFile(binary, []byte("tampered"), 0755))
	if err = Package(binary, second); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("tampered binary accepted: %v", err)
	}
}
func TestSeal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	must(t, WriteJSON(path, fixture()))
	must(t, os.WriteFile(filepath.Join(root, "hello.wapp"), []byte("pack"), 0600))
	must(t, Seal(path, "v2.0.0"))
	m, err := ReadManifest(path)
	must(t, err)
	sum, err := Digest(filepath.Join(root, "hello.wapp"))
	must(t, err)
	if m.Application.Packs[0].Version != "2.0.0" || m.Application.Packs[0].SHA256 != sum {
		t.Fatal("seal did not preserve selected version/hash")
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
		c := exec.CommandContext(ctx, binary, append([]string{"--state", state}, args...)...)
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
	output, err := invoke("recover", "Recovery")
	if err != nil || !strings.Contains(output, "Hello, Recovery!") {
		t.Fatalf("embedded recovery: %v\n%s", err, output)
	}
	files, err := os.ReadDir(cwd)
	must(t, err)
	if len(files) != 0 {
		t.Fatal("state written into caller directory")
	}
	locks, err := filepath.Glob(filepath.Join(state, "deployments", "*", "wippy.lock"))
	must(t, err)
	if len(locks) == 0 {
		t.Fatal("standalone did not seed a deployment")
	}
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

func TestSemanticVersions(t *testing.T) {
	for _, version := range []string{"1.0.0", "0.1.0-dev", "1.2.3-rc.1+build.001", "0.0.0-20260908001447-70917fa75697"} {
		if !matches(versionPattern, version) {
			t.Errorf("valid version rejected: %s", version)
		}
	}
	for _, version := range []string{"01.0.0", "1.0.0-01", "1.0.0-rc..1", "1.0.0+", "1.0.0+build..1", "1.0"} {
		if matches(versionPattern, version) {
			t.Errorf("invalid version accepted: %s", version)
		}
	}
}
