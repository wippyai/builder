// SPDX-License-Identifier: MIT
package assemble

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
