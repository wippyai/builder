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
func TestNativeComponentComposition(t *testing.T) {
	m := fixture()
	m.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/desktop", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/docker", Factory: "Docker"},
	}
	must(t, m.Validate())
	source, err := Generate(&m, true)
	must(t, err)
	if bytes.Count(source, []byte(`native0 "example.com/native/desktop"`)) != 1 || bytes.Count(source, []byte(`native1 "example.com/native/docker"`)) != 1 {
		t.Fatalf("same-module native packages were not imported once:\n%s", source)
	}
	if bytes.Count(source, []byte("native0.Desktop()")) != 1 || bytes.Count(source, []byte("native1.Docker()")) != 1 || !bytes.Contains(source, []byte("[]boot.Component{component0, component1}")) {
		t.Fatalf("same-module native factories were not composed:\n%s", source)
	}
	sharedPackage := fixture()
	sharedPackage.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/component", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/component", Factory: "Docker"},
	}
	must(t, sharedPackage.Validate())
	sharedSource, sharedError := Generate(&sharedPackage, true)
	must(t, sharedError)
	if bytes.Count(sharedSource, []byte(`native0 "example.com/native/component"`)) != 1 || bytes.Count(sharedSource, []byte("native0.Desktop()")) != 1 || bytes.Count(sharedSource, []byte("native0.Docker()")) != 1 || !bytes.Contains(sharedSource, []byte("[]boot.Component{component0, component1}")) {
		t.Fatalf("shared native package imports or factories were not deduplicated:\n%s", sharedSource)
	}
	conflicting := sharedPackage
	conflicting.Native = append([]Native{}, m.Native...)
	conflicting.Native[1].Version = "v1.1.0"
	if err := conflicting.Validate(); err == nil || !strings.Contains(err.Error(), "conflicting versions") {
		t.Fatalf("conflicting native module version accepted: %v", err)
	}
	duplicate := sharedPackage
	duplicate.Native = append([]Native{}, sharedPackage.Native...)
	duplicate.Native[1].Factory = duplicate.Native[0].Factory
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate native package and factory") {
		t.Fatalf("duplicate native package/factory accepted: %v", err)
	}
}
func TestGeneratedNativeComponentsCompileAndRunOffline(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"packs", "native/desktop", "native/docker", "native/shared", "runtime/api/boot", "runtime/cmd/app"} {
		must(t, os.MkdirAll(filepath.Join(root, directory), 0700))
	}
	must(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture/app\n\ngo 1.27.0\n\nrequire (\n\texample.com/native v1.0.0\n\tgithub.com/wippyai/runtime v1.0.0\n)\n\nreplace example.com/native => ./native\nreplace github.com/wippyai/runtime => ./runtime\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "packs/0.wapp"), []byte("fixture"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "native/go.mod"), []byte("module example.com/native\n\ngo 1.27.0\n\nrequire github.com/wippyai/runtime v1.0.0\n\nreplace github.com/wippyai/runtime => ../runtime\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "runtime/go.mod"), []byte("module github.com/wippyai/runtime\n\ngo 1.27.0\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "runtime/api/boot/boot.go"), []byte("package boot\n\ntype Component interface{}\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "runtime/cmd/app/application.go"), []byte(`package application

import (
	"context"
	"fmt"
	"github.com/wippyai/runtime/api/boot"
)

type Pack struct { Module, Version, Digest string; Data []byte }
type Bundle struct { Root string; Packs []Pack }
type Options struct { Name, Command, Mode string; Components []boot.Component; DataEnv map[string]string; Bundle Bundle }
func Run(_ context.Context, options Options, _ []string) error {
	fmt.Printf("components=%d\n", len(options.Components))
	for _, component := range options.Components { fmt.Printf("%v\n", component) }
	return nil
}
`), 0600))
	must(t, os.WriteFile(filepath.Join(root, "native/desktop/desktop.go"), []byte("package desktop\n\nimport \"github.com/wippyai/runtime/api/boot\"\n\nfunc Desktop() boot.Component { return \"desktop\" }\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "native/docker/docker.go"), []byte("package docker\n\nimport \"github.com/wippyai/runtime/api/boot\"\n\nfunc Docker() boot.Component { return \"docker\" }\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "native/shared/shared.go"), []byte("package shared\n\nimport \"github.com/wippyai/runtime/api/boot\"\n\nfunc Desktop() boot.Component { return \"shared-desktop\" }\n\nfunc Docker() boot.Component { return \"shared-docker\" }\n"), 0600))
	m := fixture()
	m.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/desktop", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/docker", Factory: "Docker"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/shared", Factory: "Desktop"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/shared", Factory: "Docker"},
	}
	source, err := Generate(&m, false)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "main.go"), source, 0600))
	command := exec.Command("go", "run", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("offline generated application failed: %v\n%s", err, output)
	}
	if string(output) != "components=4\ndesktop\ndocker\nshared-desktop\nshared-docker\n" {
		t.Fatalf("generated application received unexpected components: %q", output)
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
	applicationPackage := "./cmd/app"
	if _, err := os.Stat(filepath.Join(source, "cmd", "app", "run.go")); os.IsNotExist(err) {
		applicationPackage = "./application"
	} else if err != nil {
		t.Fatal(err)
	}
	c := exec.CommandContext(ctx, "go", "test", "-json", "-tags", strings.Join(m.Runtime.Tags, ","), applicationPackage, "-run", "^TestHubBinaryUpdate$", "-count=1")
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
