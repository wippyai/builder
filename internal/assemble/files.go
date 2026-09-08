// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

func Digest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	return atomicFile(path, mode, func(writer io.Writer) error { _, err := io.Copy(writer, bytes.NewReader(data)); return err })
}

// atomicFile replaces the destination only after its complete content is synced.
// Failures preserve an existing destination and remove the temporary file.
func atomicFile(path string, mode os.FileMode, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".builder-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(mode); err != nil {
		return err
	}
	if err = write(file); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return atomicFile(destination, mode, func(writer io.Writer) error { _, err := io.Copy(writer, input); return err })
}
