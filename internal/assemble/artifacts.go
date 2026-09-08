// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"os"
	"path/filepath"
)

// artifact identifies a release file by purpose, independently of its filename.
type artifact struct {
	Name string
	Path string
}

// artifactSet is the complete output contract for one assembled executable.
// Filename conventions belong here; build and archive code use the named fields.
type artifactSet struct {
	Binary         artifact
	Provenance     artifact
	Licenses       artifact
	GoMod          artifact
	GoSum          artifact
	RuntimePatches artifact
}

func artifactsFor(binary string) artifactSet {
	return artifactSet{
		Binary:         artifact{Name: "binary", Path: binary},
		Provenance:     artifact{Name: "provenance", Path: binary + ".provenance.json"},
		Licenses:       artifact{Name: "licenses", Path: binary + ".LICENSES.txt"},
		GoMod:          artifact{Name: "go.mod", Path: binary + ".go.mod"},
		GoSum:          artifact{Name: "go.sum", Path: binary + ".go.sum"},
		RuntimePatches: artifact{Name: "runtime-patches", Path: binary + ".runtime-patches.tar.gz"},
	}
}

// recorded returns files whose content is bound by the provenance record.
func (set artifactSet) recorded() []artifact {
	return []artifact{set.Binary, set.Licenses, set.GoMod, set.GoSum, set.RuntimePatches}
}
func (set artifactSet) all() []artifact { return append(set.recorded(), set.Provenance) }

func (set artifactSet) hashes() (map[string]string, error) {
	hashes := make(map[string]string)
	for _, file := range set.recorded() {
		sum, err := Digest(file.Path)
		if err != nil {
			return nil, err
		}
		hashes[file.Name] = sum
	}
	return hashes, nil
}
func (set artifactSet) verify(expected map[string]string) error {
	hashes, err := set.hashes()
	if err != nil {
		return err
	}
	if len(hashes) != len(expected) {
		return fmt.Errorf("provenance does not describe the complete artifact set")
	}
	for name, sum := range hashes {
		if expected[name] != sum {
			return fmt.Errorf("%s does not match provenance", name)
		}
	}
	return nil
}

// snapshot freezes files before verification so the archive contains exactly
// the bytes verified, even if another build replaces the original outputs.
func (set artifactSet) snapshot(directory string) (artifactSet, error) {
	frozen := artifactsFor(filepath.Join(directory, filepath.Base(set.Binary.Path)))
	destinations := frozen.all()
	for i, file := range set.all() {
		info, err := os.Stat(file.Path)
		if err != nil {
			return artifactSet{}, err
		}
		if !info.Mode().IsRegular() {
			return artifactSet{}, fmt.Errorf("%s must be a regular file", file.Path)
		}
		if err = copyFile(file.Path, destinations[i].Path, info.Mode().Perm()); err != nil {
			return artifactSet{}, err
		}
	}
	return frozen, nil
}
