// SPDX-License-Identifier: MIT
package assemble

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type archiveFile struct{ Path, Name string }

func archiveFiles(files []archiveFile, output string) error {
	files = append([]archiveFile(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return atomicFile(output, 0644, func(writer io.Writer) (result error) {
		compressed := gzip.NewWriter(writer)
		archive := tar.NewWriter(compressed)
		defer func() { result = errors.Join(result, archive.Close(), compressed.Close()) }()
		for _, file := range files {
			if err := appendToArchive(archive, file); err != nil {
				return err
			}
		}
		return nil
	})
}
func appendToArchive(archive *tar.Writer, file archiveFile) error {
	input, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("archive input must be a regular file: %s", file.Path)
	}
	mode := int64(0644)
	if info.Mode()&0111 != 0 {
		mode = 0755
	}
	header := &tar.Header{Name: file.Name, Mode: mode, Size: info.Size(), Typeflag: tar.TypeReg}
	if err = archive.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(archive, input)
	return err
}

// Package verifies a frozen artifact set before producing the release archive.
func Package(binary, output string) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	inputs := artifactsFor(binary)
	for _, file := range inputs.all() {
		if file.Path == output || file.Path == output+".sha256" {
			return fmt.Errorf("archive output overlaps %s input", file.Name)
		}
	}
	stage, err := os.MkdirTemp("", "wippy-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	frozen, err := inputs.snapshot(stage)
	if err != nil {
		return err
	}
	provenance, err := readProvenance(frozen.Provenance.Path)
	if err != nil {
		return err
	}
	if err = frozen.verify(provenance.Artifacts); err != nil {
		return err
	}
	var files []archiveFile
	for _, file := range frozen.all() {
		files = append(files, archiveFile{Path: file.Path, Name: filepath.Base(file.Path)})
	}
	if err = archiveFiles(files, output); err != nil {
		return err
	}
	sum, err := Digest(output)
	if err != nil {
		return err
	}
	return atomicWrite(output+".sha256", []byte(sum+"  "+filepath.Base(output)+"\n"), 0644)
}
func readProvenance(path string) (Provenance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Provenance{}, err
	}
	var record Provenance
	if err = json.Unmarshal(data, &record); err != nil {
		return Provenance{}, err
	}
	if record.Schema != 1 || record.Manifest == nil {
		return Provenance{}, fmt.Errorf("invalid build provenance")
	}
	if record.Mode != "application" && record.Mode != "toolchain" {
		return Provenance{}, fmt.Errorf("invalid build provenance mode")
	}
	if err = record.Manifest.Validate(); err != nil {
		return Provenance{}, fmt.Errorf("provenance manifest: %w", err)
	}
	return record, nil
}
