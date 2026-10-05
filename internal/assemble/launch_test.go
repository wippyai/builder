// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestApplicationLaunchManifest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		owned map[string]any
		want  string
	}{
		{name: "defaults"},
		{name: "owned only", owned: map[string]any{"command": "client"}},
		{name: "relative", state: ".hello", owned: map[string]any{"command": "client"}},
		{name: "absolute", state: filepath.Join(t.TempDir(), "hello")},
		{name: "state NUL", state: "bad\x00state", want: "state"},
		{name: "empty owned command", owned: map[string]any{}, want: "command"},
		{name: "owned command NUL", owned: map[string]any{"command": "bad\x00command"}, want: "command"},
		{name: "unknown owned field", owned: map[string]any{"command": "client", "unknown": true}, want: "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(fixture())
			must(t, err)
			var document map[string]any
			must(t, json.Unmarshal(data, &document))
			application := document["application"].(map[string]any)
			application["state"] = tc.state
			if tc.owned != nil {
				application["owned"] = tc.owned
			}
			path := filepath.Join(t.TempDir(), "wippy.build.json")
			must(t, WriteJSON(path, document))
			m, err := ReadManifest(path)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				return
			}
			must(t, err)
			source, err := Generate(m, false)
			must(t, err)
			if tc.state == "" && bytes.Contains(source, []byte("State:")) {
				t.Fatalf("unset state must not require a new runtime field:\n%s", source)
			}
			if tc.state != "" && (!bytes.Contains(source, []byte("State:")) || !bytes.Contains(source, []byte(strconv.Quote(tc.state)))) {
				t.Fatalf("generated executable lost its default state:\n%s", source)
			}
			if tc.owned == nil && bytes.Contains(source, []byte("OwnedCommand:")) {
				t.Fatalf("unset owned command must not require a new runtime field:\n%s", source)
			}
			if tc.owned != nil && !bytes.Contains(source, []byte(`OwnedCommand: "client"`)) {
				t.Fatalf("generated executable lost its owned-state command:\n%s", source)
			}
			toolchain, err := Generate(m, true)
			must(t, err)
			if bytes.Contains(toolchain, []byte("OwnedCommand:")) || bytes.Contains(toolchain, []byte("State:")) {
				t.Fatalf("source toolchain contains application launch policy:\n%s", toolchain)
			}
		})
	}
}

func TestApplicationLaunchValuesAreQuoted(t *testing.T) {
	data, err := json.Marshal(fixture())
	must(t, err)
	var document map[string]any
	must(t, json.Unmarshal(data, &document))
	application := document["application"].(map[string]any)
	application["state"] = "state\"; panic(\"injected\") //"
	application["owned"] = map[string]string{"command": "client\"; panic(\"injected\") //"}
	path := filepath.Join(t.TempDir(), "wippy.build.json")
	must(t, WriteJSON(path, document))
	m, err := ReadManifest(path)
	must(t, err)
	source, err := GenerateWithLuaCacheSeed(m, "sha256:"+strings.Repeat("a", 64))
	must(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.AllErrors)
	must(t, err)
	want := map[string]string{
		"State":        application["state"].(string),
		"OwnedCommand": application["owned"].(map[string]string)["command"],
	}
	ast.Inspect(file, func(node ast.Node) bool {
		field, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok {
			return true
		}
		expected, found := want[key.Name]
		if !found {
			return true
		}
		value, ok := field.Value.(*ast.BasicLit)
		if !ok || value.Kind != token.STRING {
			t.Fatalf("%s must be a quoted string", key.Name)
		}
		decoded, err := strconv.Unquote(value.Value)
		must(t, err)
		if decoded != expected {
			t.Fatalf("%s = %q, want %q", key.Name, decoded, expected)
		}
		delete(want, key.Name)
		return true
	})
	if len(want) != 0 {
		t.Fatalf("generated executable lost launch fields: %v", want)
	}
}

// The launch-enabled standalone CI fixture declares .hello as its default
// state and arguments as its owned-state command. Runtime tests exercise lock
// contention; this verifies Builder's generated executable and state selection.
func TestApplicationLaunchBinary(t *testing.T) {
	binary := os.Getenv("WIPPY_TEST_LAUNCH_BINARY")
	if binary == "" {
		t.Skip("requires the launch-enabled standalone fixture")
	}
	binary, err := filepath.Abs(binary)
	must(t, err)
	for _, explicit := range []bool{false, true} {
		name := "default state"
		if explicit {
			name = "explicit state"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			state := filepath.Join(directory, ".hello")
			args := []string{"run", "launch-settings"}
			if explicit {
				state = filepath.Join(directory, "explicit")
				args = append([]string{"--state", state}, args...)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = directory
			command.Env = setEnv(os.Environ(), "PATH", "/nonexistent")
			output, err := command.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "Hello, launch-settings!\n") {
				t.Fatalf("standalone launch failed: %v\n%s", err, output)
			}
			if _, err := os.Stat(filepath.Join(state, "registry.db")); err != nil {
				t.Fatalf("runtime did not use selected state %s: %v", state, err)
			}
			if explicit {
				if _, err := os.Stat(filepath.Join(directory, ".hello")); !os.IsNotExist(err) {
					t.Fatalf("explicit state also touched the default state: %v", err)
				}
			}
		})
	}
}
