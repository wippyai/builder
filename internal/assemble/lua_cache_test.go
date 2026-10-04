// SPDX-License-Identifier: MIT

package assemble

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("WIPPY_TEST_CACHE_VERIFIER") == "1" {
		if err := cacheVerifierFixture(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Simulate compilation into the selected state. Reusing it falsely reports
// hits, just as a rejected seed would if verification shared a previous state.
func cacheVerifierFixture() error {
	if len(os.Args) < 3 || os.Args[1] != "--state" {
		return errors.New("missing verification state")
	}
	state := os.Args[2]
	marker := filepath.Join(state, "compiled")
	stats := `{"compile_hits":1}`
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		stats = `{"compile_misses":1}`
		if err := os.MkdirAll(state, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte("compiled"), 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return os.WriteFile(os.Getenv("WIPPY_LUA_LINT_CACHE_STATS_FILE"), []byte(stats), 0o600)
}

func TestVerifyLuaCacheSeedNeverReusesCompiledState(t *testing.T) {
	binary, err := os.Executable()
	must(t, err)
	env := setEnv(os.Environ(), "WIPPY_TEST_CACHE_VERIFIER", "1")
	stage := t.TempDir()
	for attempt := range 4 {
		if err := verifyLuaCacheSeed(stage, binary, env); !errors.Is(err, errLuaCacheSeedMiss) {
			t.Fatalf("attempt %d falsely accepted rejected cache: %v", attempt, err)
		}
	}
}

func TestWriteVerifiedLuaCacheSeedFormatSelection(t *testing.T) {
	fatal := errors.New("build or lint failed")
	for _, tc := range []struct {
		name       string
		outcomes   []error
		wantError  error
		wantFormat luaCacheSeedFormat
	}{
		{name: "new runtime", outcomes: []error{nil}, wantFormat: luaCacheSeedZIP},
		{name: "old runtime", outcomes: []error{fmt.Errorf("cache: %w", errLuaCacheSeedMiss), nil}, wantFormat: luaCacheSeedTarGzip},
		{name: "fatal failure does not retry", outcomes: []error{fatal}, wantError: fatal, wantFormat: luaCacheSeedZIP},
		{name: "neither format accepted", outcomes: []error{errLuaCacheSeedMiss, errLuaCacheSeedMiss}, wantError: errLuaCacheSeedMiss, wantFormat: luaCacheSeedTarGzip},
		{name: "legacy fatal failure", outcomes: []error{errLuaCacheSeedMiss, fatal}, wantError: fatal, wantFormat: luaCacheSeedTarGzip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cacheRoot := filepath.Join(root, "cache")
			entry := filepath.Join(cacheRoot, "v1", "entries", strings.Repeat("a", 64))
			must(t, os.MkdirAll(entry, 0o700))
			must(t, os.WriteFile(filepath.Join(entry, "proto.luac"), []byte("proto"), 0o600))
			output := filepath.Join(root, "cache.seed")
			attempts := 0
			err := writeVerifiedLuaCacheSeed(cacheRoot, output, func(digest string) error {
				if attempts >= len(tc.outcomes) {
					t.Fatal("unexpected format retry")
				}
				data, err := os.ReadFile(output)
				must(t, err)
				if digest != fmt.Sprintf("sha256:%x", sha256.Sum256(data)) {
					t.Fatal("verification received the wrong archive digest")
				}
				result := tc.outcomes[attempts]
				attempts++
				return result
			})
			if !errors.Is(err, tc.wantError) || attempts != len(tc.outcomes) {
				t.Fatalf("got %v after %d attempts, want %v after %d", err, attempts, tc.wantError, len(tc.outcomes))
			}
			data, err := os.ReadFile(output)
			must(t, err)
			var content []byte
			if tc.wantFormat == luaCacheSeedZIP {
				archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				must(t, err)
				reader, err := archive.File[0].Open()
				must(t, err)
				content, err = io.ReadAll(reader)
				must(t, err)
				must(t, reader.Close())
			} else {
				reader, err := gzip.NewReader(bytes.NewReader(data))
				must(t, err)
				archive := tar.NewReader(reader)
				_, err = archive.Next()
				must(t, err)
				content, err = io.ReadAll(archive)
				must(t, err)
				must(t, reader.Close())
			}
			if string(content) != "proto" {
				t.Fatal("selected archive did not round trip")
			}
		})
	}
}

func TestWriteVerifiedLuaCacheSeedWithoutEntries(t *testing.T) {
	root := t.TempDir()
	must(t, writeVerifiedLuaCacheSeed(root, filepath.Join(root, "cache.seed"), func(string) error {
		t.Fatal("empty cache must not be verified")
		return nil
	}))
	if _, err := os.Stat(filepath.Join(root, "cache.seed")); !os.IsNotExist(err) {
		t.Fatalf("empty cache wrote an archive: %v", err)
	}
}

func TestLuaCacheSeedArchiveUsesIndependentZIPMembers(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	key := strings.Repeat("a", 64)
	entry := filepath.Join(cacheRoot, "v1", "entries", key)
	must(t, os.MkdirAll(entry, 0o700))
	want := map[string]string{"meta.json": `{"compile_fingerprint":"fp"}`, "proto.luac": strings.Repeat("proto", 1000)}
	for name, data := range want {
		must(t, os.WriteFile(filepath.Join(entry, name), []byte(data), 0o600))
	}
	output := filepath.Join(root, "cache.seed")
	digest, count, err := writeLuaCacheSeed(cacheRoot, output)
	must(t, err)
	data, err := os.ReadFile(output)
	must(t, err)
	if count != 1 || digest != fmt.Sprintf("sha256:%x", sha256.Sum256(data)) {
		t.Fatalf("unexpected seed identity: %s, %d entries", digest, count)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	must(t, err)
	if len(archive.File) != len(want) {
		t.Fatalf("got %d members, want %d", len(archive.File), len(want))
	}
	for _, member := range archive.File {
		if member.Method != zip.Deflate || member.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected member encoding: %s", member.Name)
		}
		reader, err := member.Open()
		must(t, err)
		content, err := io.ReadAll(reader)
		must(t, err)
		must(t, reader.Close())
		if string(content) != want[filepath.Base(member.Name)] {
			t.Fatalf("member %s does not round trip", member.Name)
		}
	}
}

func TestVerifyLuaCacheStatsRequiresObservedHits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      string
		wantError bool
	}{
		{name: "empty object", data: `{}`, wantError: true},
		{name: "disabled cache", data: `{"compile_hits":0,"compile_misses":0,"typecheck_hits":0,"typecheck_misses":0}`, wantError: true},
		{name: "compile misses", data: `{"compile_hits":1,"compile_misses":1}`, wantError: true},
		{name: "typecheck misses", data: `{"compile_hits":1,"typecheck_misses":1}`, wantError: true},
		{name: "compile-only hits", data: `{"compile_hits":1}`},
		{name: "typecheck-only hits", data: `{"typecheck_hits":1}`},
		{name: "compile and typecheck hits", data: `{"compile_hits":1,"typecheck_hits":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stats.json")
			must(t, os.WriteFile(path, []byte(tc.data), 0o600))
			err := verifyLuaCacheStats(path)
			if (err != nil) != tc.wantError {
				t.Fatalf("verifyLuaCacheStats() = %v, want error %v", err, tc.wantError)
			}
		})
	}
}
