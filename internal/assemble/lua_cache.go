// SPDX-License-Identifier: MIT

package assemble

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func writeLuaCacheSeed(cacheRoot, output string) (string, int, error) {
	entriesRoot := filepath.Join(cacheRoot, "v1", "entries")
	if _, err := os.Stat(entriesRoot); os.IsNotExist(err) {
		return "", 0, nil
	} else if err != nil {
		return "", 0, err
	}

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gz)
	fileCount := 0
	entryNames := make(map[string]struct{})
	err := filepath.WalkDir(entriesRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("Lua cache contains non-regular entry %s", path)
		}
		relative, err := filepath.Rel(cacheRoot, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		parts := strings.Split(name, "/")
		if len(parts) != 4 || parts[0] != "v1" || parts[1] != "entries" || len(parts[2]) != 64 {
			return fmt.Errorf("unexpected Lua cache path %q", name)
		}
		for _, c := range parts[2] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return fmt.Errorf("invalid Lua cache key in %q", name)
			}
		}
		switch parts[3] {
		case "meta.json", "manifest.bin", "diags.json", "proto.luac":
		default:
			return fmt.Errorf("unexpected Lua cache file %q", name)
		}
		entryNames[parts[2]] = struct{}{}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		header.Mode = 0o600
		header.Uid, header.Gid = 0, 0
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Typeflag = tar.TypeReg
		header.Format = tar.FormatUSTAR
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		fileCount++
		return nil
	})
	if err != nil {
		_ = tarWriter.Close()
		_ = gz.Close()
		return "", 0, err
	}
	if err := tarWriter.Close(); err != nil {
		_ = gz.Close()
		return "", 0, err
	}
	if err := gz.Close(); err != nil {
		return "", 0, err
	}
	if fileCount == 0 {
		return "", 0, nil
	}
	digest := sha256.Sum256(archive.Bytes())
	contentDigest := "sha256:" + hex.EncodeToString(digest[:])
	if err := atomicWrite(output, archive.Bytes(), 0o644); err != nil {
		return "", 0, err
	}
	return contentDigest, len(entryNames), nil
}
