// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
)

var licenseDocumentPattern = regexp.MustCompile(`^(LICENSE|LICENCE|COPYING|NOTICE|COPYRIGHT)([._-]|$)`)

func isLicenseDocument(name string) bool {
	if !licenseDocumentPattern.MatchString(strings.ToUpper(name)) {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case "", ".txt", ".md", ".rst", ".html", ".htm":
		return true
	default:
		return false
	}
}

func licenseNotices(source, binary string, env []string) ([]byte, error) {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return nil, fmt.Errorf("read binary module identity: %w", err)
	}
	encoded, err := capture(source, env, "go", "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var modules []goModule
	for {
		var m goModule
		err := decoder.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	modules, err = linkedModuleSources(info, modules)
	if err != nil {
		return nil, err
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	goroot, err := capture(source, env, "go", "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	license, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "LICENSE"))
	if err != nil {
		return nil, err
	}
	var notices bytes.Buffer
	notices.WriteString("Root license documents for the Go modules linked into this executable.\n\nGo toolchain and standard library\n")
	notices.Write(license)
	var missing []string
	for _, m := range modules {
		if m.Replace != nil {
			m = *m.Replace
		}
		if m.Dir == "" {
			continue
		}
		files, err := os.ReadDir(m.Dir)
		if err != nil {
			return nil, err
		}
		found := false
		for _, file := range files {
			if file.Type().IsRegular() && isLicenseDocument(file.Name()) {
				if !found {
					fmt.Fprintf(&notices, "\n%s@%s\n", m.Path, m.Version)
				}
				found = true
				data, err := os.ReadFile(filepath.Join(m.Dir, file.Name()))
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(&notices, "\n%s\n%s\n", file.Name(), data)
			}
		}
		if !found {
			missing = append(missing, m.Path)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&notices, "\nModules without a root license file; consult their source distributions:\n%s\n", strings.Join(missing, "\n"))
	}
	return notices.Bytes(), nil
}

func linkedModuleSources(info *debug.BuildInfo, modules []goModule) ([]goModule, error) {
	linked := map[string]bool{info.Main.Path: true}
	for _, dependency := range info.Deps {
		linked[dependency.Path] = true
	}
	var sources []goModule
	for _, module := range modules {
		if linked[module.Path] {
			source := module
			if module.Replace != nil {
				source = *module.Replace
			}
			if source.Dir == "" {
				return nil, fmt.Errorf("missing source directory for linked module %s", module.Path)
			}
			sources = append(sources, module)
			delete(linked, module.Path)
		}
	}
	if len(linked) != 0 {
		missing := make([]string, 0, len(linked))
		for path := range linked {
			missing = append(missing, path)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("missing source metadata for linked modules: %s", strings.Join(missing, ", "))
	}
	return sources, nil
}
