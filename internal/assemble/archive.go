// SPDX-License-Identifier: MIT
package assemble

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type archiveFile struct{ Path, Name string }

func archiveFiles(files []archiveFile, output string) (result error) {
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".archive-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	defer func() {
		if err := tw.Close(); result == nil {
			result = err
		}
		if err := gz.Close(); result == nil {
			result = err
		}
		if err := f.Close(); result == nil {
			result = err
		}
		if result == nil {
			result = os.Rename(f.Name(), output)
		}
	}()
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, item := range files {
		input, err := os.Open(item.Path)
		if err != nil {
			return err
		}
		info, err := input.Stat()
		if err != nil {
			input.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			input.Close()
			return fmt.Errorf("archive input must be a regular file")
		}
		mode := int64(0644)
		if info.Mode()&0111 != 0 {
			mode = 0755
		}
		if err = tw.WriteHeader(&tar.Header{Name: item.Name, Mode: mode, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
			input.Close()
			return err
		}
		_, err = io.Copy(tw, input)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func Package(binary, output string) error {
	var files []archiveFile
	for _, suffix := range []string{"", ".provenance.json", ".LICENSES.txt", ".go.mod", ".go.sum", ".runtime-patches.tar.gz"} {
		path := binary + suffix
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("missing binary build sidecar %s", path)
		}
		files = append(files, archiveFile{path, filepath.Base(path)})
	}
	data, err := os.ReadFile(binary + ".provenance.json")
	if err != nil {
		return err
	}
	var p Provenance
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	sum, err := Digest(binary)
	if err != nil {
		return err
	}
	if sum != p.BinarySHA256 {
		return fmt.Errorf("binary does not match provenance")
	}
	if err = archiveFiles(files, output); err != nil {
		return err
	}
	sum, err = Digest(output)
	if err != nil {
		return err
	}
	return atomicWrite(output+".sha256", []byte(sum+"  "+filepath.Base(output)+"\n"), 0644)
}
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
