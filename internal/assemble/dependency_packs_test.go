// SPDX-License-Identifier: MIT
package assemble

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// packFixture writes a manifest, a lock with one Hub module and its vendored
// pack, and a toolchain that lints cleanly and writes the root pack.
func packFixture(t *testing.T, lockHash func(string) string) (string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "wippy.build.json")
	m := fixture()
	m.Application.Packs[0].Path = "dist/hello.wapp"
	must(t, WriteJSON(path, m))
	vendored := filepath.Join(directory, ".wippy", "vendor", "wippy", "migration-0.3.21.wapp")
	must(t, os.MkdirAll(filepath.Dir(vendored), 0755))
	must(t, os.WriteFile(vendored, []byte("migration pack"), 0644))
	digest, err := Digest(vendored)
	must(t, err)
	lock := "directories:\n  modules: .wippy\n  src: ./src\nmodules:\n  - name: wippy/migration\n    version: 0.3.21\n    hash: " + lockHash(digest) + "\n"
	must(t, os.WriteFile(filepath.Join(directory, "wippy.lock"), []byte(lock), 0644))
	toolchain := filepath.Join(directory, "toolchain")
	must(t, os.WriteFile(toolchain, []byte("#!/bin/sh\nif [ \"$1\" = lint ]; then exit 0; fi\nmkdir -p dist && printf root > \"$2\"\n"), 0755))
	return path, toolchain, digest
}

func TestPackRecordsTheLockedDependencyPacks(t *testing.T) {
	path, toolchain, digest := packFixture(t, func(d string) string { return d })
	must(t, PackRoot(path, toolchain, ""))
	m, err := ReadManifest(path)
	must(t, err)
	if len(m.Application.Packs) != 2 {
		t.Fatalf("expected the root and one dependency pack, got %+v", m.Application.Packs)
	}
	if m.Application.Packs[0].Module != m.Application.Module {
		t.Fatalf("the root pack must come first: %+v", m.Application.Packs)
	}
	dependency := m.Application.Packs[1]
	if dependency.Module != "wippy/migration" || dependency.Version != "0.3.21" ||
		dependency.Path != ".wippy/vendor/wippy/migration-0.3.21.wapp" || dependency.SHA256 != digest {
		t.Fatalf("dependency pack not recorded from the lock: %+v", dependency)
	}

	must(t, PackRoot(path, toolchain, ""))
	again, err := ReadManifest(path)
	must(t, err)
	if len(again.Application.Packs) != 2 {
		t.Fatalf("repacking must not duplicate dependency packs: %+v", again.Application.Packs)
	}
}

func TestPackRefusesAVendoredPackThatDiffersFromTheLock(t *testing.T) {
	path, toolchain, _ := packFixture(t, func(string) string { return strings.Repeat("0", 64) })
	err := PackRoot(path, toolchain, "")
	if err == nil || !strings.Contains(err.Error(), "does not match wippy.lock") {
		t.Fatalf("expected a lock mismatch error, got %v", err)
	}
}
