// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// lockFile is the part of wippy.lock that selects dependency packs.
type lockFile struct {
	Directories struct {
		Modules string `yaml:"modules"`
	} `yaml:"directories"`
	Modules []struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
		Hash    string `yaml:"hash"`
	} `yaml:"modules"`
}

// lockedPacks returns the vendored packs of every module wippy.lock selects,
// each checked against the hash the lock recorded. A directory without a lock
// has no dependency packs.
func lockedPacks(directory string) ([]Pack, error) {
	data, err := os.ReadFile(filepath.Join(directory, "wippy.lock"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lock lockFile
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("read wippy.lock: %w", err)
	}
	modules := lock.Directories.Modules
	if modules == "" {
		modules = ".wippy"
	}
	packs := make([]Pack, 0, len(lock.Modules))
	for _, module := range lock.Modules {
		parts := strings.Split(module.Name, "/")
		if len(parts) != 2 || !matches(modulePattern, module.Name) || !matches(`v?`+versionPattern, module.Version) {
			return nil, fmt.Errorf("wippy.lock selects an invalid module %q@%q", module.Name, module.Version)
		}
		path := filepath.ToSlash(filepath.Join(modules, "vendor", parts[0], parts[1]+"-"+module.Version+".wapp"))
		digest, err := Digest(filepath.Join(directory, path))
		if err != nil {
			return nil, fmt.Errorf("dependency pack %s: %w (run wippy install)", path, err)
		}
		if digest != module.Hash {
			return nil, fmt.Errorf("dependency pack %s does not match wippy.lock", path)
		}
		packs = append(packs, Pack{Module: module.Name, Version: strings.TrimPrefix(module.Version, "v"), Path: path, SHA256: digest})
	}
	return packs, nil
}
