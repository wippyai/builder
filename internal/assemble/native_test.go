// SPDX-License-Identifier: MIT
package assemble

import (
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

func TestAssembleHostFromFrozenInputs(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repository")
	must(t, os.MkdirAll(repository, 0755))
	env := os.Environ()
	for key, value := range map[string]string{"GIT_AUTHOR_NAME": "Builder test", "GIT_AUTHOR_EMAIL": "test@example.com", "GIT_COMMITTER_NAME": "Builder test", "GIT_COMMITTER_EMAIL": "test@example.com"} {
		env = setEnv(env, key, value)
	}
	must(t, run(repository, env, "git", "init", "--quiet"))
	must(t, os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/wippyai/runtime\n\ngo 1.27.0\n"), 0644))
	must(t, run(repository, env, "git", "add", "go.mod"))
	must(t, run(repository, env, "git", "commit", "--quiet", "-m", "fixture"))
	commit, err := capture(repository, env, "git", "rev-parse", "HEAD")
	must(t, err)
	t.Setenv("WIPPY_BUILD_RUNTIME_REPOSITORY", repository)
	m := nativeFixture()
	m.Runtime.Commit = strings.TrimSpace(string(commit))
	root := t.TempDir()
	manifestPath := filepath.Join(root, "wippy.build.json")
	packPath := filepath.Join(root, m.Application.Packs[0].Path)
	must(t, os.WriteFile(packPath, []byte("verified pack"), 0600))
	m.Application.Packs[0].SHA256, err = Digest(packPath)
	must(t, err)
	must(t, WriteJSON(manifestPath, m))
	stage := t.TempDir()
	inputs, err := freezeInputs(manifestPath, &m, artifactsFor(filepath.Join(root, "hello")), stage, false)
	must(t, err)
	must(t, os.WriteFile(packPath, []byte("changed after verification"), 0600))
	source, err := prepareSource(stage, &m, inputs, false, env)
	must(t, err)
	embedded, err := os.ReadFile(filepath.Join(source, "cmd", "assembled", "packs", "0.wapp"))
	must(t, err)
	if string(embedded) != "verified pack" {
		t.Fatal("assembly did not use frozen pack")
	}
	generated, err := os.ReadFile(filepath.Join(source, "cmd", "assembled", "main.go"))
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
	if strings.Count(string(calls), "mod edit -require=example.com/native@v1.0.0") != 1 || strings.Count(string(calls), "list -mod=readonly") != 3 || !strings.Contains(string(calls), "mod verify") {
		t.Fatalf("incorrect module or package verification:\n%s", calls)
	}
	err = prepareDependencies(root, setEnv(env, "WIPPY_TEST_WRONG_OWNER", "1"), &m)
	if err == nil || !strings.Contains(err.Error(), "not provided by pinned module") {
		t.Fatalf("unverified second package accepted: %v", err)
	}
}
