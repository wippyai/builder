// SPDX-License-Identifier: MIT
package assemble

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func nativeFixture() Manifest {
	m := fixture()
	m.Application.Data = map[string]string{"Z_DB": "db/z.sqlite", "A_ROOT": "files"}
	m.Native = []Native{
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/events", Factory: "Component"},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/launch", Factory: "Host", Host: true, Private: true},
		{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/launch", Factory: "Observer"},
	}
	return m
}

// runtimeProxy serves a minimal runtime module through a file-based Go module
// proxy, so assembly resolves the runtime exactly as a release build does.
func runtimeProxy(t *testing.T, version string) []string {
	t.Helper()
	proxy := t.TempDir()
	base := filepath.Join(proxy, "github.com", "wippyai", "runtime", "@v")
	must(t, os.MkdirAll(base, 0755))
	mod := []byte("module github.com/wippyai/runtime\n\ngo 1.27.0\n")
	must(t, os.WriteFile(filepath.Join(base, version+".mod"), mod, 0644))
	must(t, os.WriteFile(filepath.Join(base, version+".info"), []byte(`{"Version":"`+version+`"}`), 0644))
	must(t, os.WriteFile(filepath.Join(base, "list"), []byte(version+"\n"), 0644))
	archive, err := os.Create(filepath.Join(base, version+".zip"))
	must(t, err)
	writer := zip.NewWriter(archive)
	entry, err := writer.Create("github.com/wippyai/runtime@" + version + "/go.mod")
	must(t, err)
	_, err = entry.Write(mod)
	must(t, err)
	must(t, writer.Close())
	must(t, archive.Close())
	env := os.Environ()
	for key, value := range map[string]string{"GOPROXY": "file://" + filepath.ToSlash(proxy), "GOSUMDB": "off", "GOFLAGS": "", "GOWORK": "off"} {
		env = setEnv(env, key, value)
	}
	return env
}

func TestAssembleHostFromFrozenInputs(t *testing.T) {
	env := runtimeProxy(t, "v0.1.0")
	m := nativeFixture()
	m.Runtime.Version = "v0.1.0"
	root := t.TempDir()
	manifestPath := filepath.Join(root, "wippy.build.json")
	packPath := filepath.Join(root, m.Application.Packs[0].Path)
	must(t, os.WriteFile(packPath, []byte("verified pack"), 0600))
	var err error
	m.Application.Packs[0].SHA256, err = Digest(packPath)
	must(t, err)
	must(t, WriteJSON(manifestPath, m))
	stage := t.TempDir()
	inputs, err := freezeInputs(manifestPath, &m, artifactsFor(filepath.Join(root, "hello")), stage, false)
	must(t, err)
	must(t, os.WriteFile(packPath, []byte("changed after verification"), 0600))
	source, err := prepareModule(stage, &m, inputs, false, env)
	must(t, err)
	embedded, err := os.ReadFile(filepath.Join(source, "packs", "0.wapp"))
	must(t, err)
	if string(embedded) != "verified pack" {
		t.Fatal("assembly did not use frozen pack")
	}
	required, err := os.ReadFile(filepath.Join(source, "go.mod"))
	must(t, err)
	if !bytes.Contains(required, []byte("github.com/wippyai/runtime v0.1.0")) {
		t.Fatalf("assembly does not require the pinned runtime module:\n%s", required)
	}
	generated, err := os.ReadFile(filepath.Join(source, "main.go"))
	must(t, err)
	want, err := Generate(&m, false)
	must(t, err)
	if !slices.Equal(generated, want) || !bytes.Contains(generated, []byte("Host: component1")) {
		t.Fatalf("assembled executable lost native host:\n%s", generated)
	}
}

func TestNativeManifestDecodingAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Manifest)
		want   string
	}{
		{name: "shared module and package"},
		{name: "no host", change: func(m *Manifest) { m.Native[1].Host = false }},
		{name: "conflicting versions", change: func(m *Manifest) { m.Native[2].Version = "v1.1.0" }, want: "conflicting versions"},
		{name: "duplicate factory", change: func(m *Manifest) { m.Native[2].Factory = "Host" }, want: "duplicate native component"},
		{name: "multiple hosts", change: func(m *Manifest) { m.Native[0].Host = true }, want: "more than one native host"},
		{name: "wrong module owner", change: func(m *Manifest) { m.Native[0].Package = "example.com/other/events" }, want: "invalid native component"},
		{name: "invalid environment name", change: func(m *Manifest) { m.Application.Data["bad-name"] = "db" }, want: "invalid data environment binding"},
		{name: "absolute data path", change: func(m *Manifest) { m.Application.Data["A_ROOT"] = "/outside" }, want: "invalid data environment binding"},
		{name: "escaping data path", change: func(m *Manifest) { m.Application.Data["A_ROOT"] = "../outside" }, want: "invalid data environment binding"},
		{name: "empty data path", change: func(m *Manifest) { m.Application.Data["A_ROOT"] = "" }, want: "invalid data environment binding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := nativeFixture()
			if tc.change != nil {
				tc.change(&m)
			}
			path := filepath.Join(t.TempDir(), "manifest.json")
			must(t, WriteJSON(path, m))
			got, err := ReadManifest(path)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				return
			}
			must(t, err)
			if got.Native[1].Host != m.Native[1].Host || got.Application.Data["Z_DB"] != "db/z.sqlite" {
				t.Fatal("lost executable fields")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	must(t, WriteJSON(path, nativeFixture()))
	data, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, bytes.Replace(data, []byte(`"data":`), []byte(`"data_env":`), 1), 0600))
	if _, err = ReadManifest(path); err == nil || !strings.Contains(err.Error(), `unknown field "data_env"`) {
		t.Fatalf("legacy data alias accepted: %v", err)
	}
}

func TestGeneratedMainGolden(t *testing.T) {
	for _, name := range []string{"application", "host", "toolchain", "host-cache"} {
		t.Run(name, func(t *testing.T) {
			m := nativeFixture()
			if name == "application" {
				m.Native = nil
			}
			var source []byte
			var err error
			if name == "host-cache" {
				source, err = GenerateWithLuaCacheSeed(&m, "sha256:"+strings.Repeat("b", 64))
			} else {
				source, err = Generate(&m, name == "toolchain")
			}
			must(t, err)
			path := filepath.Join("testdata", name+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				must(t, os.MkdirAll("testdata", 0755))
				must(t, os.WriteFile(path, source, 0644))
			}
			want, err := os.ReadFile(path)
			must(t, err)
			if !bytes.Equal(source, want) {
				t.Fatalf("generated main differs from %s:\n%s", path, source)
			}
		})
	}
}

func TestPrepareDependenciesSharedModuleVerifiesEveryFactory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	root := t.TempDir()
	log := filepath.Join(root, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$WIPPY_TEST_GO_CALLS"
if [ "$1" = list ] && [ "$3" = -m ]; then
  if [ "$WIPPY_TEST_RUNTIME_REPLACED" = 1 ]; then
    printf '{"Path":"github.com/wippyai/runtime","Version":"v0.1.0","Replace":{"Path":"../runtime"}}\n'
  else
    printf '{"Path":"github.com/wippyai/runtime","Version":"v0.1.0"}\n'
  fi
  exit 0
fi
if [ "$1" = list ]; then
  for argument do package="$argument"; done
  version=v1.0.0
  if [ "$WIPPY_TEST_WRONG_OWNER" = 1 ] && [ "$package" = example.com/native/launch ]; then version=v1.1.0; fi
  printf '{"ImportPath":"%s","Module":{"Path":"example.com/native","Version":"%s"}}\n' "$package" "$version"
fi
`
	must(t, os.WriteFile(filepath.Join(root, "go"), []byte(script), 0700))
	t.Setenv("PATH", root)
	env := setEnv(os.Environ(), "WIPPY_TEST_GO_CALLS", log)
	m := nativeFixture()
	must(t, prepareDependencies(root, env, &m))
	calls, err := os.ReadFile(log)
	must(t, err)
	if strings.Count(string(calls), "mod edit -require=example.com/native@v1.0.0") != 1 || strings.Count(string(calls), "list -mod=readonly -tags") != 3 || strings.Count(string(calls), "list -mod=readonly -m -json github.com/wippyai/runtime") != 1 || !strings.Contains(string(calls), "mod verify") {
		t.Fatalf("incorrect module or package verification:\n%s", calls)
	}
	err = prepareDependencies(root, setEnv(env, "WIPPY_TEST_WRONG_OWNER", "1"), &m)
	if err == nil || !strings.Contains(err.Error(), "not provided by pinned module") {
		t.Fatalf("unverified second package accepted: %v", err)
	}
	err = prepareDependencies(root, setEnv(env, "WIPPY_TEST_RUNTIME_REPLACED", "1"), &m)
	if err == nil || !strings.Contains(err.Error(), "not resolved from the module cache") {
		t.Fatalf("replaced runtime accepted: %v", err)
	}
}
