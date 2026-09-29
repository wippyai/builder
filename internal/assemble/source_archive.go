// SPDX-License-Identifier: MIT

package assemble

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func extractRuntimeArchive(archive []byte, destination string) error {
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read runtime source archive: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := path.Clean(header.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) || strings.ContainsRune(name, 0) {
			return fmt.Errorf("invalid runtime source path %q", header.Name)
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("runtime source path escapes destination: %q", header.Name)
		}
		mode := os.FileMode(header.Mode) & 0o777
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return fmt.Errorf("invalid runtime source directory %q", header.Name)
			}
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 {
				return fmt.Errorf("invalid runtime source size for %q", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			link := path.Clean(header.Linkname)
			linkTarget := path.Clean(path.Join(path.Dir(name), header.Linkname))
			if path.IsAbs(header.Linkname) || link == ".." || strings.HasPrefix(link, "../") || linkTarget == ".." || strings.HasPrefix(linkTarget, "../") {
				return fmt.Errorf("runtime source symlink escapes destination: %q", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(header.Linkname), target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported runtime source entry %q", header.Name)
		}
	}
}
