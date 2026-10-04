// SPDX-License-Identifier: MIT

package assemble

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errLuaCacheSeedMiss = errors.New("embedded Lua cache seed was not used")

type luaCacheSeedFormat string

const (
	luaCacheSeedZIP     luaCacheSeedFormat = "zip"
	luaCacheSeedTarGzip luaCacheSeedFormat = "tar.gz"
)

// Verify each checker profile in fresh state so a previous verification run
// cannot supply the entries that the embedded seed is expected to provide.
func verifyLuaCacheSeed(stage, binary string, env []string) error {
	// A format retry must not read entries compiled by the previous attempt.
	verification, err := os.MkdirTemp(stage, "cache-verification-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(verification)
	for _, profile := range []string{"default", "strict", "non-strict"} {
		state := filepath.Join(verification, profile)
		statsPath := filepath.Join(verification, "lua-cache-stats-"+profile+".json")
		verifyEnv := setEnv(env, "WIPPY_LUA_LINT_CACHE_STATS_FILE", statsPath)
		args := []string{"--state", state, "wippy", "lint", "--set", "lua.cache.dir=" + filepath.Join(state, "cache", "lua")}
		if profile != "default" {
			args = append(args, "--set", "lua.type_system.enabled=true", "--set", "lua.type_system.strict="+strconv.FormatBool(profile == "strict"))
		}
		if err := run(stage, verifyEnv, binary, args...); err != nil {
			return fmt.Errorf("verify %s embedded Lua cache seed: %w", profile, err)
		}
		if err := verifyLuaCacheStats(statsPath); err != nil {
			return fmt.Errorf("verify %s embedded Lua cache hits: %w", profile, err)
		}
	}
	return nil
}

func verifyLuaCacheStats(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var stats struct {
		CompileHits     uint64 `json:"compile_hits"`
		CompileMisses   uint64 `json:"compile_misses"`
		TypecheckHits   uint64 `json:"typecheck_hits"`
		TypecheckMisses uint64 `json:"typecheck_misses"`
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return err
	}
	if stats.CompileHits == 0 && stats.TypecheckHits == 0 {
		return fmt.Errorf("%w: no Lua cache entries were read", errLuaCacheSeedMiss)
	}
	if stats.CompileMisses != 0 || stats.TypecheckMisses != 0 {
		return fmt.Errorf("%w: compile misses %d, typecheck misses %d", errLuaCacheSeedMiss, stats.CompileMisses, stats.TypecheckMisses)
	}
	return nil
}

func writeLuaCacheSeed(cacheRoot, output string) (string, int, error) {
	return writeLuaCacheSeedArchive(cacheRoot, output, luaCacheSeedZIP)
}

// Prefer individually compressed members. Older pinned runtimes read tar.gz,
// so retry that format only on observed cache misses, never on build/lint errors.
// The caller must verify each attempt in fresh state before exporting a binary.
func writeVerifiedLuaCacheSeed(cacheRoot, output string, verify func(string) error) error {
	var failures []error
	for _, format := range []luaCacheSeedFormat{luaCacheSeedZIP, luaCacheSeedTarGzip} {
		digest, _, err := writeLuaCacheSeedArchive(cacheRoot, output, format)
		if err != nil {
			return fmt.Errorf("create %s embedded Lua cache seed: %w", format, err)
		}
		if digest == "" {
			return nil
		}
		if err = verify(digest); err == nil {
			return nil
		}
		if !errors.Is(err, errLuaCacheSeedMiss) {
			return err
		}
		failures = append(failures, fmt.Errorf("%s: %w", format, err))
	}
	return errors.Join(failures...)
}

func writeLuaCacheSeedArchive(cacheRoot, output string, format luaCacheSeedFormat) (string, int, error) {
	entriesRoot := filepath.Join(cacheRoot, "v1", "entries")
	if _, err := os.Stat(entriesRoot); os.IsNotExist(err) {
		return "", 0, nil
	} else if err != nil {
		return "", 0, err
	}

	var archive bytes.Buffer
	var zipWriter *zip.Writer
	var gz *gzip.Writer
	var tarWriter *tar.Writer
	switch format {
	case luaCacheSeedZIP:
		zipWriter = zip.NewWriter(&archive)
	case luaCacheSeedTarGzip:
		gz = gzip.NewWriter(&archive)
		tarWriter = tar.NewWriter(gz)
	default:
		return "", 0, fmt.Errorf("unsupported Lua cache seed format %q", format)
	}
	closeArchive := func() error {
		if zipWriter != nil {
			return zipWriter.Close()
		}
		return errors.Join(tarWriter.Close(), gz.Close())
	}
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
		var member io.Writer
		if zipWriter != nil {
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0o600)
			member, err = zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}
		} else {
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
			member = tarWriter
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(member, file)
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
		_ = closeArchive()
		return "", 0, err
	}
	if err := closeArchive(); err != nil {
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
