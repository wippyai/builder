// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Input struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Pack struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}
type Runtime struct {
	Repository string   `json:"repository"`
	Commit     string   `json:"commit"`
	Go         string   `json:"go"`
	Tags       []string `json:"tags"`
	Patches    []Input  `json:"patches,omitempty"`
}
type Application struct {
	Module  string            `json:"module"`
	Command string            `json:"command"`
	Mode    string            `json:"mode"`
	DataEnv map[string]string `json:"data_env,omitempty"`
	Packs   []Pack            `json:"packs"`
}
type Native struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Package string `json:"package"`
	Factory string `json:"factory"`
	Private bool   `json:"private,omitempty"`
}
type Manifest struct {
	Schema      int         `json:"schema"`
	Name        string      `json:"name"`
	Runtime     Runtime     `json:"runtime"`
	Application Application `json:"application"`
	Native      []Native    `json:"native,omitempty"`
}

func matches(pattern, value string) bool {
	return regexp.MustCompile("^(?:" + pattern + ")$").MatchString(value)
}
func local(path string) bool {
	return filepath.IsLocal(path) && !strings.ContainsRune(path, 0) && !strings.Contains(strings.ReplaceAll(path, "\\", "/"), "../")
}

const versionPattern = `[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`
const modulePattern = `[a-z][a-z0-9-]*/[a-z][a-z0-9-]*`

func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&m); err != nil {
		return nil, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("manifest must contain one JSON object")
	}
	if err = m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
func (m *Manifest) Validate() error {
	if m.Schema != 1 || !matches(`[a-z][a-z0-9_-]*`, m.Name) {
		return fmt.Errorf("invalid schema or executable name")
	}
	r := m.Runtime
	if !strings.HasPrefix(r.Repository, "https://") || !matches(`[0-9a-f]{40}`, r.Commit) || !matches(`[0-9]+\.[0-9]+\.[0-9]+`, r.Go) || r.Tags == nil {
		return fmt.Errorf("runtime requires HTTPS source, exact commit, Go version and tags")
	}
	for _, tag := range r.Tags {
		if !matches(`[A-Za-z0-9_]+`, tag) {
			return fmt.Errorf("invalid build tag %q", tag)
		}
	}
	for _, p := range r.Patches {
		if !local(p.Path) || !matches(`[0-9a-f]{64}`, p.SHA256) {
			return fmt.Errorf("patch requires local path and SHA-256")
		}
	}
	app := m.Application
	if !matches(modulePattern, app.Module) || app.Command == "" || (app.Mode != "base" && app.Mode != "bootstrap") {
		return fmt.Errorf("invalid application identity, command or mode")
	}
	modules := map[string]bool{}
	for _, p := range app.Packs {
		if !matches(modulePattern, p.Module) || !matches(`v?`+versionPattern, p.Version) || !local(p.Path) || !matches(`[0-9a-f]{64}`, p.SHA256) || modules[p.Module] {
			return fmt.Errorf("invalid or duplicate pack %q", p.Module)
		}
		modules[p.Module] = true
	}
	if !modules[app.Module] {
		return fmt.Errorf("root application pack is missing")
	}
	for name, path := range app.DataEnv {
		if !matches(`[A-Z][A-Z0-9_]*`, name) || !local(path) {
			return fmt.Errorf("invalid data environment binding %q", name)
		}
	}
	modules = map[string]bool{}
	for _, n := range m.Native {
		if !matches(`[A-Za-z0-9._~/-]+`, n.Module) || !strings.Contains(n.Module, ".") || modules[n.Module] || !matches(`v`+versionPattern, n.Version) || !matches(`[A-Z][A-Za-z0-9_]*`, n.Factory) || !matches(`[A-Za-z0-9._~/-]+`, n.Package) || (n.Package != n.Module && !strings.HasPrefix(n.Package, n.Module+"/")) {
			return fmt.Errorf("invalid or duplicate native component %q", n.Module)
		}
		modules[n.Module] = true
	}
	return nil
}
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0644)
}
func Seal(path, version, mode string) error {
	m, err := ReadManifest(path)
	if err != nil {
		return err
	}
	for i := range m.Application.Packs {
		p := &m.Application.Packs[i]
		if p.Module == m.Application.Module && version != "" {
			p.Version = strings.TrimPrefix(version, "v")
		}
		p.SHA256, err = Digest(filepath.Join(filepath.Dir(path), p.Path))
		if err != nil {
			return err
		}
	}
	if mode != "" {
		m.Application.Mode = mode
	}
	if err = m.Validate(); err != nil {
		return err
	}
	return WriteJSON(path, m)
}

// PackRoot creates a source snapshot with a published identity. Multi-module
// applications supply independently prepared canonical packs to Build instead.
func PackRoot(path, toolchain, version string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	m, err := ReadManifest(path)
	if err != nil {
		return err
	}
	if len(m.Application.Packs) != 1 {
		return fmt.Errorf("pack requires one source root; prepare dependency packs independently")
	}
	p := m.Application.Packs[0]
	if version != "" {
		p.Version = strings.TrimPrefix(version, "v")
	}
	if !matches(versionPattern, p.Version) {
		return fmt.Errorf("invalid application version")
	}
	if !strings.ContainsAny(toolchain, `/\`) {
		toolchain, err = exec.LookPath(toolchain)
		if err != nil {
			return err
		}
	}
	toolchain, err = filepath.Abs(toolchain)
	if err != nil {
		return err
	}
	parts := strings.Split(p.Module, "/")
	output := filepath.Join(filepath.Dir(path), p.Path)
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err = run(filepath.Dir(path), nil, toolchain, "pack", output, "--meta", "namespace="+strings.Join(parts, "."), "--meta", "name="+parts[1], "--meta", "version="+p.Version, "--silent"); err != nil {
		return err
	}
	return Seal(path, p.Version, "")
}
