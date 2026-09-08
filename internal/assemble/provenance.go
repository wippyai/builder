// SPDX-License-Identifier: MIT
package assemble

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
)

// Provenance binds the manifest, assembler identity and every release artifact.
// Artifact keys describe purposes, not filenames, so output names may vary.
type Provenance struct {
	Schema    int               `json:"schema"`
	Mode      string            `json:"mode"`
	Manifest  *Manifest         `json:"manifest"`
	Builder   BuilderIdentity   `json:"builder"`
	Artifacts map[string]string `json:"artifacts"`
}
type BuilderIdentity struct {
	Revision string `json:"revision,omitempty"`
	Modified bool   `json:"modified"`
	Go       string `json:"go"`
}

func builderIdentity() BuilderIdentity {
	var identity BuilderIdentity
	if info, ok := debug.ReadBuildInfo(); ok {
		identity.Go = info.GoVersion
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				identity.Revision = setting.Value
			case "vcs.modified":
				identity.Modified = setting.Value == "true"
			}
		}
	}
	return identity
}

func exportBuild(source, binary string, outputs artifactSet, m *Manifest, inputs map[string]string, env []string, toolchain bool) error {
	notices, err := licenseNotices(source, env)
	if err != nil {
		return err
	}
	if err = copyFile(binary, outputs.Binary.Path, 0755); err != nil {
		return err
	}
	if err = atomicWrite(outputs.Licenses.Path, notices, 0644); err != nil {
		return err
	}
	if err = copyFile(filepath.Join(source, "go.mod"), outputs.GoMod.Path, 0644); err != nil {
		return err
	}
	if err = copyFile(filepath.Join(source, "go.sum"), outputs.GoSum.Path, 0644); err != nil {
		return err
	}
	var patches []archiveFile
	for i, patch := range m.Runtime.Patches {
		patches = append(patches, archiveFile{Path: inputs[patch.Path], Name: fmt.Sprintf("%d-%s", i, filepath.Base(patch.Path))})
	}
	if err = archiveFiles(patches, outputs.RuntimePatches.Path); err != nil {
		return err
	}
	hashes, err := outputs.hashes()
	if err != nil {
		return err
	}
	provenance := Provenance{Schema: 1, Mode: "application", Manifest: m, Builder: builderIdentity(), Artifacts: hashes}
	if toolchain {
		provenance.Mode = "toolchain"
	}
	// Write the record last: an incomplete output set cannot pass packaging.
	return WriteJSON(outputs.Provenance.Path, provenance)
}
