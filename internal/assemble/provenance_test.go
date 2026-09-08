// SPDX-License-Identifier: MIT
package assemble

import "testing"

func TestBuilderIdentityWithoutGitMetadata(t *testing.T) {
	previous := buildRevision
	buildRevision = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { buildRevision = previous })
	identity := builderIdentity()
	if identity.Revision != buildRevision {
		t.Fatalf("archive build revision = %q, want %q", identity.Revision, buildRevision)
	}
	if identity.Go == "" {
		t.Fatal("missing Go toolchain identity")
	}
}
