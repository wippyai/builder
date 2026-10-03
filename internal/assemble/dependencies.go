// SPDX-License-Identifier: MIT
package assemble

import (
	"encoding/json"
	"fmt"
	"strings"
)

type goModule struct {
	Path, Version, Dir string
	Replace            *goModule
}
type goPackage struct {
	ImportPath string
	Module     *goModule
}

func prepareDependencies(source string, env []string, m *Manifest) error {
	for _, component := range uniqueNativeModules(m.Native) {
		if err := run(source, env, "go", "mod", "edit", "-require="+component.Module+"@"+component.Version); err != nil {
			return err
		}
	}
	if err := run(source, env, "go", "mod", "tidy"); err != nil {
		return err
	}
	data, err := capture(source, env, "go", "list", "-mod=readonly", "-m", "-json", m.Runtime.Module)
	if err != nil {
		return err
	}
	var runtime goModule
	if err = json.Unmarshal(data, &runtime); err != nil {
		return err
	}
	if runtime.Path != m.Runtime.Module || runtime.Replace != nil {
		return fmt.Errorf("runtime module %s is not resolved from the module cache", m.Runtime.Module)
	}
	for _, component := range m.Native {
		data, err := capture(source, env, "go", "list", "-mod=readonly", "-tags", strings.Join(m.Runtime.Tags, ","), "-json", "--", component.Package)
		if err != nil {
			return err
		}
		var selected goPackage
		if err = json.Unmarshal(data, &selected); err != nil {
			return err
		}
		if err = verifyNativePackage(component, selected); err != nil {
			return err
		}
	}
	return run(source, env, "go", "mod", "verify")
}

// uniqueNativeModules keeps one requirement per module. Manifest validation
// ensures all factories from a module select the same version.
func uniqueNativeModules(components []Native) []Native {
	seen := make(map[string]bool, len(components))
	var unique []Native
	for _, component := range components {
		if !seen[component.Module] {
			seen[component.Module] = true
			unique = append(unique, component)
		}
	}
	return unique
}

// verifyNativePackage checks the package's resolved module owner and version,
// including imports from repositories containing nested modules.
func verifyNativePackage(component Native, selected goPackage) error {
	module := selected.Module
	if selected.ImportPath != component.Package || module == nil || module.Path != component.Module || module.Version != component.Version || module.Replace != nil {
		return fmt.Errorf("native import %s is not provided by pinned module %s@%s", component.Package, component.Module, component.Version)
	}
	return nil
}
