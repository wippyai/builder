// SPDX-License-Identifier: MIT
package assemble

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPackLintFailurePreservesArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "wippy.build.json")
	m := fixture()
	m.Application.Packs[0].Path = "application.wapp"
	must(t, WriteJSON(path, m))
	manifest, err := os.ReadFile(path)
	must(t, err)
	pack := filepath.Join(directory, "application.wapp")
	must(t, os.WriteFile(pack, []byte("previous pack"), 0644))
	toolchain := filepath.Join(directory, "toolchain")
	must(t, os.WriteFile(toolchain, []byte("#!/bin/sh\nif [ \"$1\" = lint ]; then exit 23; fi\nprintf overwritten > application.wapp\n"), 0755))
	if err := PackRoot(path, toolchain, ""); err == nil {
		t.Fatal("accepted failed source validation")
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if !slices.Equal(manifest, after) {
		t.Fatal("failed validation changed manifest")
	}
	data, err := os.ReadFile(pack)
	must(t, err)
	if string(data) != "previous pack" {
		t.Fatal("failed validation replaced pack")
	}
}

func TestFailedAtomicWritePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result")
	must(t, os.WriteFile(path, []byte("previous"), 0644))
	failure := errors.New("write interrupted")
	err := atomicFile(path, 0644, func(writer io.Writer) error {
		_, writeErr := io.WriteString(writer, "incomplete")
		if writeErr != nil {
			return writeErr
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("unexpected failure: %v", err)
	}
	data, err := os.ReadFile(path)
	must(t, err)
	if string(data) != "previous" {
		t.Fatal("incomplete output replaced previous artifact")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	must(t, err)
	if len(files) != 1 {
		t.Fatal("temporary output leaked")
	}
}
func TestNativePackageMustBelongToPinnedModule(t *testing.T) {
	component := Native{Module: "example.com/native", Version: "v1.0.0", Package: "example.com/native/watch", Factory: "Component"}
	selected := goPackage{ImportPath: component.Package, Module: &goModule{Path: component.Module, Version: component.Version}}
	must(t, verifyNativePackage(component, selected))
	for _, module := range []*goModule{
		nil,
		{Path: "example.com/native/watch", Version: "v1.0.0"},
		{Path: component.Module, Version: "v1.1.0"},
		{Path: component.Module, Version: component.Version, Replace: &goModule{Dir: "local"}},
	} {
		if verifyNativePackage(component, goPackage{ImportPath: component.Package, Module: module}) == nil {
			t.Errorf("accepted unpinned import owner: %+v", module)
		}
	}
}
func TestGitEnvironmentDoesNotRedirectBuildRepositories(t *testing.T) {
	env := gitEnvironment([]string{"GIT_DIR=/caller/.git", "GIT_WORK_TREE=/caller", "GIT_INDEX_FILE=/caller/index", "GIT_OBJECT_DIRECTORY=/caller/objects", "GH_TOKEN=fixture", "HOME=/home/fixture"})
	if !slices.Equal(env, []string{"GH_TOKEN=fixture", "HOME=/home/fixture"}) {
		t.Fatalf("unexpected Git environment: %v", env)
	}
}

func TestExecutableLintArgs(t *testing.T) {
	state := filepath.Join(t.TempDir(), "validation-state")
	want := []string{"--state", state, "wippy", "lint", "--set", "lua.type_system.enabled=true", "--set", "lua.type_system.strict=true"}
	if got := executableLintArgs(state); !slices.Equal(got, want) {
		t.Fatalf("embedded validation arguments = %q, want %q", got, want)
	}
}

func TestRejectsDuplicateBuildInputs(t *testing.T) {
	m := fixture()
	m.Runtime.Patches = []Input{{Path: m.Application.Packs[0].Path, SHA256: m.Application.Packs[0].SHA256}}
	if m.Validate() == nil {
		t.Fatal("accepted pack and patch sharing an input path")
	}
	m = fixture()
	m.Runtime.Patches = []Input{{Path: "runtime.patch", SHA256: m.Application.Packs[0].SHA256}, {Path: "./runtime.patch", SHA256: m.Application.Packs[0].SHA256}}
	if m.Validate() == nil {
		t.Fatal("accepted duplicate patch paths")
	}
}

func TestRuntimePatchUsesFrozenSource(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "wippy.build.json")
	patchPath := filepath.Join(root, "runtime.patch")
	m := fixture()
	pack := []byte("pack")
	must(t, os.WriteFile(filepath.Join(root, m.Application.Packs[0].Path), pack, 0644))
	packDigest, err := Digest(filepath.Join(root, m.Application.Packs[0].Path))
	must(t, err)
	m.Application.Packs[0].SHA256 = packDigest
	patch := []byte("original runtime patch")
	must(t, os.WriteFile(patchPath, patch, 0644))
	patchDigest, err := Digest(patchPath)
	must(t, err)
	m.Runtime.Patches = []Input{{Path: "runtime.patch", SHA256: patchDigest}}
	must(t, WriteJSON(manifestPath, m))
	decoded, err := ReadManifest(manifestPath)
	must(t, err)
	stage := t.TempDir()
	inputs, err := freezeInputs(manifestPath, decoded, artifactsFor(filepath.Join(root, "bee")), stage, false)
	must(t, err)
	must(t, os.WriteFile(patchPath, []byte("tampered runtime patch"), 0644))
	archivePath := filepath.Join(root, "patches.tar.gz")
	must(t, archiveRuntimePatches(decoded.Runtime.Patches, inputs, archivePath))

	compressed, err := gzip.NewReader(bytes.NewReader(mustRead(t, archivePath)))
	must(t, err)
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	header, err := archive.Next()
	must(t, err)
	if header.Name != "0-runtime.patch" {
		t.Fatalf("unexpected frozen patch name %q", header.Name)
	}
	archived, err := io.ReadAll(archive)
	must(t, err)
	if !bytes.Equal(archived, patch) {
		t.Fatalf("archive did not use the verified frozen patch: %q", archived)
	}
	if _, err = archive.Next(); err != io.EOF {
		t.Fatalf("unexpected additional patch archive entry: %v", err)
	}
}

func TestPrepareSourceAppliesFrozenRuntimePatch(t *testing.T) {
	repository := t.TempDir()
	must(t, run(repository, nil, "git", "init"))
	must(t, os.MkdirAll(filepath.Join(repository, "cmd", "assembled"), 0755))
	must(t, os.WriteFile(filepath.Join(repository, "runtime.txt"), []byte("before\n"), 0644))
	must(t, run(repository, nil, "git", "add", "."))
	must(t, run(repository, nil, "git", "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "runtime"))
	commit, err := capture(repository, nil, "git", "rev-parse", "HEAD")
	must(t, err)

	root := t.TempDir()
	manifestPath := filepath.Join(root, "wippy.build.json")
	patchPath := filepath.Join(root, "runtime.patch")
	patch := []byte("diff --git a/runtime.txt b/runtime.txt\nindex df967b9..3e75765 100644\n--- a/runtime.txt\n+++ b/runtime.txt\n@@ -1 +1 @@\n-before\n+after\n")
	must(t, os.WriteFile(patchPath, patch, 0644))
	patchDigest, err := Digest(patchPath)
	must(t, err)
	m := fixture()
	m.Runtime.Commit = strings.TrimSpace(string(commit))
	m.Runtime.Patches = []Input{{Path: "runtime.patch", SHA256: patchDigest}}
	must(t, WriteJSON(manifestPath, m))
	stage := t.TempDir()
	inputs, err := freezeInputs(manifestPath, &m, artifactsFor(filepath.Join(root, "bee")), stage, true)
	must(t, err)
	must(t, os.WriteFile(patchPath, []byte("invalid patch"), 0644))
	t.Setenv("WIPPY_BUILD_RUNTIME_REPOSITORY", repository)
	source, err := prepareSource(stage, &m, inputs, true)
	must(t, err)
	patched, err := os.ReadFile(filepath.Join(source, "runtime.txt"))
	must(t, err)
	if string(patched) != "after\n" {
		t.Fatalf("runtime patch was not applied from its frozen source: %q", patched)
	}
}

func TestRuntimePatchChecksumBeforeTools(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	m := fixture()
	packPath := filepath.Join(root, m.Application.Packs[0].Path)
	must(t, os.WriteFile(packPath, []byte("pack"), 0644))
	packDigest, err := Digest(packPath)
	must(t, err)
	m.Application.Packs[0].SHA256 = packDigest
	m.Runtime.Patches = []Input{{Path: "runtime.patch", SHA256: strings.Repeat("a", 64)}}
	must(t, WriteJSON(path, m))
	must(t, os.WriteFile(filepath.Join(root, "runtime.patch"), []byte("tampered"), 0644))
	t.Setenv("PATH", "/nonexistent")
	err = Build(path, filepath.Join(root, "binary"), false)
	if err == nil || !strings.Contains(err.Error(), "input checksum mismatch: runtime.patch") {
		t.Fatalf("expected runtime patch checksum preflight, got %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	return data
}

func TestPackCannotOverwriteManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wippy.build.json")
	m := fixture()
	m.Application.Packs[0].Path = filepath.Base(path)
	must(t, WriteJSON(path, m))
	before, err := os.ReadFile(path)
	must(t, err)
	err = PackRoot(path, filepath.Join(t.TempDir(), "missing-toolchain"), "")
	if err == nil || err.Error() != "pack output overlaps the build manifest" {
		t.Fatalf("expected overlap preflight, got %v", err)
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if !slices.Equal(before, after) {
		t.Fatal("pack changed its input manifest")
	}
}
