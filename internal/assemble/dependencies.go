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
	for _, component := range m.Native {
		if err := run(source, env, "go", "mod", "edit", "-require="+component.Module+"@"+component.Version); err != nil {
			return err
		}
	}
	if len(m.Native) > 0 {
		if err := run(source, env, "go", "mod", "tidy"); err != nil {
			return err
		}
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

// verifyNativePackage checks the package's resolved module owner and version,
// including imports from repositories containing nested modules.
func verifyNativePackage(component Native, selected goPackage) error {
	module := selected.Module
	if selected.ImportPath != component.Package || module == nil || module.Path != component.Module || module.Version != component.Version || module.Replace != nil {
		return fmt.Errorf("native import %s is not provided by pinned module %s@%s", component.Package, component.Module, component.Version)
	}
	return nil
}
