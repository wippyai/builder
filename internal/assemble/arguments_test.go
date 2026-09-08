// SPDX-License-Identifier: MIT
package assemble

import (
	"context"
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
	values := []string{"--state-dir", "value with spaces\nand a newline", "", "--", "雪", `tail="quoted"'`}
	var expected strings.Builder
	for _, value := range values {
		expected.WriteString(strconv.Itoa(len(value)))
		expected.WriteByte(':')
		expected.WriteString(value)
	}
	routes := map[string][]string{
		"application run":       {"--command", "arguments", "run"},
		"application separator": {"--command", "arguments", "--"},
		"canonical runtime":     {"runtime", "run", "--silent", "--", "arguments"},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			state := filepath.Join(t.TempDir(), "state")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			args := append([]string{"--state-dir", state}, route...)
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
}
