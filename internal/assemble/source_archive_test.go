// SPDX-License-Identifier: MIT

package assemble

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRuntimeArchive(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "runtime")
	archive := makeSourceArchive(t, "cmd/assembled/main.go", []byte("package main\n"))
	if err := extractRuntimeArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "cmd", "assembled", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("extracted source = %q", data)
	}
}

func TestExtractRuntimeArchiveSkipsPAXGlobalHeader(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "runtime")
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{
		Name:       "pax_global_header",
		Typeflag:   tar.TypeXGlobalHeader,
		Format:     tar.FormatPAX,
		PAXRecords: map[string]string{"comment": "pinned source commit"},
	}); err != nil {
		t.Fatal(err)
	}
	data := []byte("package main\n")
	if err := writer.WriteHeader(&tar.Header{Name: "cmd/assembled/main.go", Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractRuntimeArchive(archive.Bytes(), destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "cmd", "assembled", "main.go")); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRuntimeArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "runtime")
	archive := makeSourceArchive(t, "../escape", []byte("outside"))
	if err := extractRuntimeArchive(archive, destination); err == nil {
		t.Fatal("source archive traversal was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote outside its destination: %v", err)
	}
}

func TestExtractRuntimeArchiveRejectsSymlinkParentEscape(t *testing.T) {
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archive := makeSourceArchive(t, "linked/escape", []byte("outside"))
	if err := extractRuntimeArchive(archive, destination); err == nil {
		t.Fatal("source archive followed an escaping parent symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote outside its destination: %v", err)
	}
}

func makeSourceArchive(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
