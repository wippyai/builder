// SPDX-License-Identifier: MIT
package assemble

import (
	"runtime/debug"
	"testing"
)

func TestNoticesFollowLinkedModules(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/runtime"},
		Deps: []*debug.Module{{Path: "example.com/linked"}},
	}
	modules := []goModule{
		{Path: "example.com/runtime", Dir: "/runtime"},
		{Path: "example.com/test-only", Dir: "/test"},
		{Path: "example.com/linked", Replace: &goModule{Path: "example.com/replacement", Dir: "/replacement"}},
	}
	sources, err := linkedModuleSources(info, modules)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Path != info.Main.Path || sources[1].Path != info.Deps[0].Path {
		t.Fatalf("unexpected linked module inventory: %+v", sources)
	}
	if _, err := linkedModuleSources(info, modules[:2]); err == nil {
		t.Fatal("accepted missing linked module metadata")
	}
	modules[2].Replace.Dir = ""
	if _, err := linkedModuleSources(info, modules); err == nil {
		t.Fatal("accepted missing replacement source directory")
	}
}

func TestLicenseDocumentNames(t *testing.T) {
	for _, name := range []string{"LICENSE", "LICENSE-MIT", "LICENSE-APACHE", "LICENSE.txt", "license.md", "COPYING.LESSER.txt", "NOTICE", "COPYRIGHT.rst"} {
		if !isLicenseDocument(name) {
			t.Errorf("rejected license document %q", name)
		}
	}
	for _, name := range []string{"license_test.go", "license.go", "LICENSE.py", "NOTICE.sh", "README.md", "LICENSED.txt"} {
		if isLicenseDocument(name) {
			t.Errorf("accepted non-document %q", name)
		}
	}
}
