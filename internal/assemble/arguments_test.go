// SPDX-License-Identifier: MIT
package assemble

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestArguments checks argv through the assembled application's host, CLI and
// Lua process boundary.
func TestArguments(t *testing.T) {
	binary := os.Getenv("WIPPY_TEST_BINARY")
	if binary == "" {
		t.Skip("requires assembled hello binary")
	}
	binary, err := filepath.Abs(binary)
	must(t, err)
	values := []string{"--state", "value with spaces\nand a newline", "", "--", "雪", `tail="quoted"'`}
	var expected strings.Builder
	for _, value := range values {
		expected.WriteString(strconv.Itoa(len(value)))
		expected.WriteByte(':')
		expected.WriteString(value)
	}
	routes := map[string][]string{
		"runtime passthrough": {"wippy", "run", "--silent", "--", "arguments"},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			state := filepath.Join(t.TempDir(), "state")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			args := append([]string{"--state", state}, route...)
			args = append(args, values...)
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = directory
			env := setEnv(os.Environ(), "PATH", "/nonexistent")
			env = setEnv(env, "HOME", directory)
			command.Env = setEnv(env, "XDG_CONFIG_HOME", filepath.Join(directory, "config"))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("argument forwarding failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), expected.String()+"\n") {
				t.Fatalf("argv changed across runtime boundaries:\n%s", output)
			}
		})
	}
	for name, strict := range map[string]bool{"strict typecheck": true, "non-strict typecheck": false} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			state := filepath.Join(directory, "state")
			statsPath := filepath.Join(directory, "lua-cache-stats.json")
			must(t, os.MkdirAll(state, 0700))
			config := "version: \"1.0\"\nlua:\n  type_system:\n    enabled: true\n    strict: " + strconv.FormatBool(strict) + "\n"
			must(t, os.WriteFile(filepath.Join(state, ".wippy.yaml"), []byte(config), 0600))
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			args := []string{"--state", state, "--state"}
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = directory
			command.Env = []string{
				"PATH=/nonexistent",
				"HOME=" + directory,
				"XDG_CONFIG_HOME=" + filepath.Join(directory, "config"),
				"TMPDIR=" + os.Getenv("TMPDIR"),
				"WIPPY_LUA_CACHE_STATS_FILE=" + statsPath,
			}
			output, err := command.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "Hello, --state!\n") {
				t.Fatalf("application arguments changed: %v\n%s", err, output)
			}
			statsData, err := os.ReadFile(statsPath)
			if err != nil {
				t.Fatalf("read runtime cache stats: %v\n%s", err, output)
			}
			var stats struct {
				CompileHits     uint64 `json:"compile_hits"`
				CompileMisses   uint64 `json:"compile_misses"`
				TypecheckHits   uint64 `json:"typecheck_hits"`
				TypecheckMisses uint64 `json:"typecheck_misses"`
			}
			if err := json.Unmarshal(statsData, &stats); err != nil {
				t.Fatalf("decode runtime cache stats: %v", err)
			}
			if stats.CompileHits == 0 || stats.CompileMisses != 0 || stats.TypecheckHits == 0 || stats.TypecheckMisses != 0 {
				t.Fatalf("application boot did not consume its embedded Lua cache: %+v", stats)
			}
		})
	}
}
