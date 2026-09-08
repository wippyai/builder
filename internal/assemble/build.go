// SPDX-License-Identifier: MIT
package assemble

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func command(dir string, env []string, name string, args ...string) *exec.Cmd {
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
func Digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".builder-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWrite(dst, data, mode)
}

type Provenance struct {
	Schema       int       `json:"schema"`
	Mode         string    `json:"mode"`
	Manifest     *Manifest `json:"manifest"`
	BinarySHA256 string    `json:"binary_sha256"`
	GoModSHA256  string    `json:"go_mod_sha256"`
	GoSumSHA256  string    `json:"go_sum_sha256"`
}
type goModule struct {
	Path    string
	Version string
	Dir     string
	Replace *goModule
}

func Build(path, output string, toolchain bool) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	m, err := ReadManifest(path)
	if err != nil {
		return err
	}
	base := filepath.Dir(path)
	inputs := append([]Input{}, m.Runtime.Patches...)
	if !toolchain {
		for _, p := range m.Application.Packs {
			inputs = append(inputs, Input{p.Path, p.SHA256})
		}
	}
	for _, p := range inputs {
		sum, err := Digest(filepath.Join(base, p.Path))
		if err != nil {
			return err
		}
		if sum != p.SHA256 {
			return fmt.Errorf("input checksum mismatch: %s", p.Path)
		}
	}
	env := os.Environ()
	for key, value := range map[string]string{"GOWORK": "off", "GOTOOLCHAIN": "go" + m.Runtime.Go, "CGO_ENABLED": "1", "GOFLAGS": ""} {
		env = setEnv(env, key, value)
	}
	var private []string
	for _, n := range m.Native {
		if n.Private {
			private = append(private, n.Module)
		}
	}
	if len(private) > 0 {
		for _, key := range []string{"GOPRIVATE", "GONOPROXY", "GONOSUMDB"} {
			existing, err := capture("", env, "go", "env", key)
			if err != nil {
				return err
			}
			values := append([]string{strings.TrimSpace(string(existing))}, private...)
			env = setEnv(env, key, strings.Trim(strings.Join(values, ","), ","))
		}
	}
	stage, err := os.MkdirTemp("", "wippy-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// Freeze checksummed inputs before fetching source so concurrent source work
	// cannot change the patches or packs selected by this build.
	verified := make(map[string]string, len(inputs))
	for i, input := range inputs {
		frozen := filepath.Join(stage, "inputs", fmt.Sprintf("%d", i))
		if err = copyFile(filepath.Join(base, input.Path), frozen, 0600); err != nil {
			return err
		}
		sum, err := Digest(frozen)
		if err != nil {
			return err
		}
		if sum != input.SHA256 {
			return fmt.Errorf("input changed during verification: %s", input.Path)
		}
		verified[input.Path] = frozen
	}

	source := filepath.Join(stage, "runtime")
	repository := m.Runtime.Repository
	if override := os.Getenv("WIPPY_BUILD_RUNTIME_REPOSITORY"); override != "" {
		repository = override
	}
	if err = run("", nil, "git", "clone", "--no-checkout", "--filter=blob:none", repository, source); err != nil {
		return err
	}
	if err = run(source, nil, "git", "checkout", "--detach", m.Runtime.Commit); err != nil {
		return err
	}
	for _, p := range m.Runtime.Patches {
		if err = run(source, nil, "git", "apply", "--check", verified[p.Path]); err != nil {
			return err
		}
		if err = run(source, nil, "git", "apply", verified[p.Path]); err != nil {
			return err
		}
	}
	for _, n := range m.Native {
		if err = run(source, env, "go", "mod", "edit", "-require="+n.Module+"@"+n.Version); err != nil {
			return err
		}
	}
	entry := filepath.Join(source, "cmd", "assembled")
	if err = os.MkdirAll(filepath.Join(entry, "packs"), 0755); err != nil {
		return err
	}
	if !toolchain {
		for i, p := range m.Application.Packs {
			if err = copyFile(verified[p.Path], filepath.Join(entry, "packs", fmt.Sprintf("%d.wapp", i)), 0644); err != nil {
				return err
			}
		}
	}
	generated, err := Generate(m, toolchain)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(entry, "main.go"), generated, 0644); err != nil {
		return err
	}
	if len(m.Native) > 0 {
		if err = run(source, env, "go", "mod", "tidy"); err != nil {
			return err
		}
	}
	for _, n := range m.Native {
		data, err := capture(source, env, "go", "list", "-m", "-json", n.Module)
		if err != nil {
			return err
		}
		var selected goModule
		if err = json.Unmarshal(data, &selected); err != nil {
			return err
		}
		if selected.Version != n.Version || selected.Replace != nil {
			return fmt.Errorf("native module selection changed: %s", n.Module)
		}
	}
	if err = run(source, env, "go", "mod", "verify"); err != nil {
		return err
	}
	binary := filepath.Join(stage, m.Name)
	if err = run(source, env, "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-tags", strings.Join(m.Runtime.Tags, ","), "-o", binary, "./cmd/assembled"); err != nil {
		return err
	}
	p := Provenance{Schema: 1, Mode: "application", Manifest: m}
	if toolchain {
		p.Mode = "toolchain"
	}
	if p.BinarySHA256, err = Digest(binary); err != nil {
		return err
	}
	if p.GoModSHA256, err = Digest(filepath.Join(source, "go.mod")); err != nil {
		return err
	}
	if p.GoSumSHA256, err = Digest(filepath.Join(source, "go.sum")); err != nil {
		return err
	}
	notices, err := licenseNotices(source, env)
	if err != nil {
		return err
	}
	if err = copyFile(binary, output, 0755); err != nil {
		return err
	}
	if err = WriteJSON(output+".provenance.json", p); err != nil {
		return err
	}
	if err = atomicWrite(output+".LICENSES.txt", notices, 0644); err != nil {
		return err
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if err = copyFile(filepath.Join(source, name), output+"."+name, 0644); err != nil {
			return err
		}
	}
	var patches []archiveFile
	for i, p := range m.Runtime.Patches {
		patches = append(patches, archiveFile{verified[p.Path], fmt.Sprintf("%d-%s", i, filepath.Base(p.Path))})
	}
	return archiveFiles(patches, output+".runtime-patches.tar.gz")
}
