// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func command(dir string, env []string, name string, args ...string) *exec.Cmd {
	if name == "git" {
		env = gitEnvironment(env)
		args = append([]string{"-c", "core.autocrlf=false", "-c", "core.hooksPath=" + os.DevNull}, args...)
	}
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = env
	return c
}
func run(dir string, env []string, name string, args ...string) error {
	c := command(dir, env, name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
func capture(dir string, env []string, name string, args ...string) ([]byte, error) {
	c := command(dir, env, name, args...)
	c.Stderr = os.Stderr
	return c.Output()
}
func setEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, s := range env {
		if !strings.HasPrefix(s, key+"=") {
			out = append(out, s)
		}
	}
	return append(out, key+"="+value)
}

// Preserve credentials and proxy settings, but never inherit repository handles
// from a parent Git hook or workspace. Go's Git subprocesses use this too.
func gitEnvironment(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	var result []string
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX":
			continue
		}
		result = append(result, entry)
	}
	return result
}
func buildEnvironment(m *Manifest) ([]string, error) {
	env := gitEnvironment(nil)
	for key, value := range map[string]string{"GOWORK": "off", "GOTOOLCHAIN": "go" + m.Runtime.Go, "CGO_ENABLED": "1", "GOFLAGS": ""} {
		env = setEnv(env, key, value)
	}
	var private []string
	for _, component := range m.Native {
		if component.Private {
			private = append(private, component.Module)
		}
	}
	if len(private) == 0 {
		return env, nil
	}
	for _, key := range []string{"GOPRIVATE", "GONOPROXY", "GONOSUMDB"} {
		current, err := capture("", env, "go", "env", key)
		if err != nil {
			return nil, err
		}
		values := append([]string{strings.TrimSpace(string(current))}, private...)
		env = setEnv(env, key, strings.Trim(strings.Join(values, ","), ","))
	}
	return env, nil
}
