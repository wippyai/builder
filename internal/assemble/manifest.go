// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
	Baseline string            `json:"baseline,omitempty"`
	Module   string            `json:"module"`
	Command  string            `json:"command"`
	Mode     string            `json:"mode"`
	DataEnv  map[string]string `json:"data_env,omitempty"`
	Packs    []Pack            `json:"packs"`
}
type Native struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Package string `json:"package"`
	Factory string `json:"factory"`
	Private bool   `json:"private,omitempty"`
	Launch  bool   `json:"launch,omitempty"`
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

const numberPattern = `(?:0|[1-9][0-9]*)`
const prereleasePattern = `(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`
const versionPattern = numberPattern + `\.` + numberPattern + `\.` + numberPattern +
	`(?:-` + prereleasePattern + `(?:\.` + prereleasePattern + `)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?`
const modulePattern = `[a-z][a-z0-9-]*/[a-z][a-z0-9-]*`

func validImportPath(path string) bool {
	if !matches(`[A-Za-z0-9._~/-]+`, path) {
		return false
	}
	parts := strings.Split(path, "/")
	if !strings.Contains(parts[0], ".") || !matches(`[A-Za-z0-9][A-Za-z0-9.-]*`, parts[0]) {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

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
	repository, err := url.Parse(r.Repository)
	if err != nil || repository.Scheme != "https" || repository.Hostname() == "" || repository.User != nil || repository.RawQuery != "" || repository.Fragment != "" {
		return fmt.Errorf("runtime repository must be an HTTPS Git URL without credentials, query or fragment")
	}
	if !matches(`[0-9a-f]{40}`, r.Commit) || !matches(numberPattern+`\.`+numberPattern+`\.`+numberPattern, r.Go) || r.Tags == nil {
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
	paths := make(map[string]bool)
	for _, patch := range r.Patches {
		if paths[filepath.Clean(patch.Path)] {
			return fmt.Errorf("duplicate build input path %q", patch.Path)
		}
		paths[filepath.Clean(patch.Path)] = true
	}
	app := m.Application
	if app.Baseline != "" && app.Baseline != "activated" && app.Baseline != "embedded" {
		return fmt.Errorf("application baseline must be activated or embedded")
	}
	if !matches(modulePattern, app.Module) || app.Command == "" || (app.Mode != "base" && app.Mode != "bootstrap") {
		return fmt.Errorf("invalid application identity, command or mode")
	}
	modules := map[string]bool{}
	for _, p := range app.Packs {
		if !matches(modulePattern, p.Module) || !matches(`v?`+versionPattern, p.Version) || !local(p.Path) || !matches(`[0-9a-f]{64}`, p.SHA256) || modules[p.Module] {
			return fmt.Errorf("invalid or duplicate pack %q", p.Module)
		}
		if paths[filepath.Clean(p.Path)] {
			return fmt.Errorf("duplicate build input path %q", p.Path)
		}
		paths[filepath.Clean(p.Path)] = true
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
	launchers := 0
	nativeModules := map[string]string{}
	nativeComponents := map[string]bool{}
	for _, n := range m.Native {
		if n.Launch {
			launchers++
		}
		if !validImportPath(n.Module) || !matches(`v`+versionPattern, n.Version) || !matches(`[A-Z][A-Za-z0-9_]*`, n.Factory) || !validImportPath(n.Package) || (n.Package != n.Module && !strings.HasPrefix(n.Package, n.Module+"/")) {
			return fmt.Errorf("invalid native component %q", n.Module)
		}
		if version, exists := nativeModules[n.Module]; exists && version != n.Version {
			return fmt.Errorf("native module %q has conflicting versions %s and %s", n.Module, version, n.Version)
		}
		component := n.Package + "\x00" + n.Factory
		if nativeComponents[component] {
			return fmt.Errorf("duplicate native package and factory %q", n.Package)
		}
		nativeModules[n.Module] = n.Version
		nativeComponents[component] = true
	}
	if launchers > 1 {
		return fmt.Errorf("only one native factory may supply application launch")
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
// applications supply independently prepared packs to Build.
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
	p.Version = strings.TrimPrefix(p.Version, "v")
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
	if output == path {
		return fmt.Errorf("pack output overlaps the build manifest")
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err = run(filepath.Dir(path), nil, toolchain, "pack", output, "--meta", "namespace="+strings.Join(parts, "."), "--meta", "name="+parts[1], "--meta", "version="+p.Version, "--silent"); err != nil {
		return err
	}
	return Seal(path, p.Version, "")
}
