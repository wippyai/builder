// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Build compiles a pinned runtime module and native component selection. Application
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
	source, err := prepareModule(stage, manifest, inputs, toolchain, env)
	if err != nil {
		return err
	}
	if err = prepareDependencies(source, env, manifest); err != nil {
		return err
	}
	binary := filepath.Join(stage, manifest.Name)
	if err = goBuild(source, env, manifest, binary); err != nil {
		return err
	}
	if !toolchain {
		validationState := filepath.Join(stage, "validation-state")
		luaCache := filepath.Join(validationState, "cache", "lua")
		args := append([]string{"--state", validationState, "wippy"}, strictLintArgs(luaCache)...)
		if err = run(stage, env, binary, args...); err != nil {
			return fmt.Errorf("validate embedded application: %w", err)
		}
		args = []string{"--state", validationState, "wippy", "lint", "--set", "lua.cache.dir=" + luaCache}
		if err = run(stage, env, binary, args...); err != nil {
			return fmt.Errorf("warm embedded application cache: %w", err)
		}
		args = []string{"--state", validationState, "wippy", "lint", "--set", "lua.cache.dir=" + luaCache,
			"--set", "lua.type_system.enabled=true", "--set", "lua.type_system.strict=false"}
		if err = run(stage, env, binary, args...); err != nil {
			return fmt.Errorf("warm non-strict embedded application cache: %w", err)
		}
		seedPath := filepath.Join(source, "lua-cache.seed")
		seedDigest, _, err := writeLuaCacheSeed(luaCache, seedPath)
		if err != nil {
			return fmt.Errorf("create embedded Lua cache seed: %w", err)
		}
		if seedDigest != "" {
			generated, err := GenerateWithLuaCacheSeed(manifest, seedDigest)
			if err != nil {
				return err
			}
			if err = atomicWrite(filepath.Join(source, "main.go"), generated, 0644); err != nil {
				return err
			}
			if err = goBuild(source, env, manifest, binary); err != nil {
				return err
			}
			if err = verifyLuaCacheSeed(stage, binary, env); err != nil {
				return err
			}
		}
	}
	return exportBuild(source, binary, outputs, manifest, env, toolchain)
}

// goBuild compiles the application with the runtime's version variables
// stamped from the runtime module the build resolved, so the binary reports
// the runtime it runs.
func goBuild(source string, env []string, manifest *Manifest, binary string) error {
	resolved, err := capture(source, env, "go", "list", "-mod=readonly", "-m", "-f", "{{.Version}}", manifest.Runtime.Module)
	if err != nil {
		return fmt.Errorf("resolve runtime module version: %w", err)
	}
	return run(source, env, "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false",
		"-tags", strings.Join(manifest.Runtime.Tags, ","),
		"-ldflags", runtimeVersionFlags(manifest.Runtime, strings.TrimSpace(string(resolved))), "-o", binary, ".")
}

// runtimeVersionFlags sets the runtime's reported version to the resolved
// module version and its commit to the manifest's pin.
func runtimeVersionFlags(runtime Runtime, resolved string) string {
	pkg := runtime.Module + "/api/version"
	return "-X " + pkg + ".Version=" + resolved + " -X " + pkg + ".Commit=" + runtime.Version
}

func strictLintArgs(luaCache string) []string {
	args := []string{"lint"}
	if luaCache != "" {
		args = append(args, "--set", "lua.cache.dir="+luaCache)
	}
	return append(args, "--set", "lua.type_system.enabled=true", "--set", "lua.type_system.strict=true")
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

// prepareModule writes the executable's own Go module: a generated main
// package and its embedded packs, requiring the runtime and native components
// as ordinary module dependencies resolved through the Go module cache.
func prepareModule(stage string, m *Manifest, inputs map[string]string, toolchain bool, env []string) (string, error) {
	source := filepath.Join(stage, "module")
	if err := os.MkdirAll(source, 0o755); err != nil {
		return "", err
	}
	goMod := fmt.Sprintf("module wippy.build/%s\n\ngo %s\n", m.Name, m.Runtime.Go)
	if err := atomicWrite(filepath.Join(source, "go.mod"), []byte(goMod), 0644); err != nil {
		return "", err
	}
	if !toolchain {
		for i, pack := range m.Application.Packs {
			if err := copyFile(inputs[pack.Path], filepath.Join(source, "packs", fmt.Sprintf("%d.wapp", i)), 0644); err != nil {
				return "", err
			}
		}
	}
	generated, err := Generate(m, toolchain)
	if err != nil {
		return "", err
	}
	if err = atomicWrite(filepath.Join(source, "main.go"), generated, 0644); err != nil {
		return "", err
	}
	if err = run(source, env, "go", "get", m.Runtime.Module+"@"+m.Runtime.Version); err != nil {
		return "", err
	}
	return source, nil
}
