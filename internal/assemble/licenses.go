// SPDX-License-Identifier: MIT
package assemble

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func licenseNotices(source string, env []string) ([]byte, error) {
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
	notices.WriteString("Third-party license inventory for this build.\n\nGo toolchain and standard library\n")
	notices.Write(license)
	var missing []string
	pattern := regexp.MustCompile(`^(LICENSE|LICENCE|COPYING|NOTICE|COPYRIGHT)([._-]|$)`)
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
			if file.Type().IsRegular() && pattern.MatchString(strings.ToUpper(file.Name())) {
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
