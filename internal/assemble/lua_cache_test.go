// SPDX-License-Identifier: MIT

package assemble

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyLuaCacheStatsRequiresObservedHits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      string
		wantError bool
	}{
		{name: "empty object", data: `{}`, wantError: true},
		{name: "disabled cache", data: `{"compile_hits":0,"compile_misses":0,"typecheck_hits":0,"typecheck_misses":0}`, wantError: true},
		{name: "compile misses", data: `{"compile_hits":1,"compile_misses":1}`, wantError: true},
		{name: "typecheck misses", data: `{"compile_hits":1,"typecheck_misses":1}`, wantError: true},
		{name: "compile-only hits", data: `{"compile_hits":1}`},
		{name: "typecheck-only hits", data: `{"typecheck_hits":1}`},
		{name: "compile and typecheck hits", data: `{"compile_hits":1,"typecheck_hits":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stats.json")
			must(t, os.WriteFile(path, []byte(tc.data), 0o600))
			err := verifyLuaCacheStats(path)
			if (err != nil) != tc.wantError {
				t.Fatalf("verifyLuaCacheStats() = %v, want error %v", err, tc.wantError)
			}
		})
	}
}
