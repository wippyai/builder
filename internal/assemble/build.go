// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Build compiles a pinned runtime and native component selection. Application
// builds embed the verified packs; toolchain builds expose the normal Wippy CLI.
func Build(manifestPath, output string, toolchain bool) error {
	manifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	manifest, err := ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	outputs := artifactsFor(output)
	stage, err := os.MkdirTemp("", "wippy-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	inputs, err := freezeInputs(manifestPath, manifest, outputs, stage, toolchain)
	if err != nil {
		return err
	}
	env, err := buildEnvironment(manifest)
	if err != nil {
		return err
	}
	source, err := prepareSource(stage, manifest, inputs, toolchain)
	if err != nil {
		return err
	}
	if err = prepareDependencies(source, env, manifest); err != nil {
		return err
	}
	binary := filepath.Join(stage, manifest.Name)
	if err = run(source, env, "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-tags", strings.Join(manifest.Runtime.Tags, ","), "-o", binary, "./cmd/assembled"); err != nil {
		return err
	}
	if !toolchain {
		args := append([]string{"--state", filepath.Join(stage, "validation-state"), "wippy"}, strictLintArgs()...)
		if err = run(stage, env, binary, args...); err != nil {
			return fmt.Errorf("validate embedded application: %w", err)
		}
	}
	return exportBuild(source, binary, outputs, manifest, env, toolchain)
}

func strictLintArgs() []string {
	return []string{"lint", "--set", "lua.type_system.enabled=true", "--set", "lua.type_system.strict=true"}
}

func freezeInputs(manifestPath string, m *Manifest, outputs artifactSet, stage string, toolchain bool) (map[string]string, error) {
	var inputs []Input
	if !toolchain {
		for _, pack := range m.Application.Packs {
			inputs = append(inputs, Input{Path: pack.Path, SHA256: pack.SHA256})
		}
	}
	protected := map[string]bool{manifestPath: true}
	base := filepath.Dir(manifestPath)
	for _, input := range inputs {
		protected[filepath.Join(base, input.Path)] = true
	}
	for _, output := range outputs.all() {
		if protected[output.Path] {
			return nil, fmt.Errorf("%s output overlaps a build input", output.Name)
		}
	}
	verified := make(map[string]string, len(inputs))
	for i, input := range inputs {
		frozen := filepath.Join(stage, "inputs", fmt.Sprintf("%d", i))
		if err := copyFile(filepath.Join(base, input.Path), frozen, 0600); err != nil {
			return nil, err
		}
		sum, err := Digest(frozen)
		if err != nil {
			return nil, err
		}
		if sum != input.SHA256 {
			return nil, fmt.Errorf("input checksum mismatch: %s", input.Path)
		}
		verified[input.Path] = frozen
	}
	return verified, nil
}

func prepareSource(stage string, m *Manifest, inputs map[string]string, toolchain bool) (string, error) {
	source := filepath.Join(stage, "runtime")
	repository := m.Runtime.Repository
	if override := os.Getenv("WIPPY_BUILD_RUNTIME_REPOSITORY"); override != "" {
		repository = override
	}
	if err := run("", nil, "git", "clone", "--no-checkout", "--filter=blob:none", repository, source); err != nil {
		return "", err
	}
	if err := run(source, nil, "git", "checkout", "--detach", m.Runtime.Commit); err != nil {
		return "", err
	}
	entry := filepath.Join(source, "cmd", "assembled")
	if !toolchain {
		for i, pack := range m.Application.Packs {
			if err := copyFile(inputs[pack.Path], filepath.Join(entry, "packs", fmt.Sprintf("%d.wapp", i)), 0644); err != nil {
				return "", err
			}
		}
	}
	generated, err := Generate(m, toolchain)
	if err != nil {
		return "", err
	}
	if err = atomicWrite(filepath.Join(entry, "main.go"), generated, 0644); err != nil {
		return "", err
	}
	return source, nil
}
