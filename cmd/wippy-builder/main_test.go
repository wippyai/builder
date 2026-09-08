// SPDX-License-Identifier: MIT
package main

import "testing"

func TestRejectsOptionsThatWouldOtherwiseBeIgnored(t *testing.T) {
	for _, args := range [][]string{
		{"build", "manifest.json", "--output", "binary", "--version", "2.0.0"},
		{"validate", "manifest.json", "--mode", "bootstrap"},
		{"pack", "manifest.json", "--output", "binary"},
	} {
		if err := execute(args); err == nil {
			t.Errorf("accepted unsupported options: %v", args)
		}
	}
}

func TestHelpSucceedsForEveryCommand(t *testing.T) {
	for _, name := range []string{"build", "toolchain", "pack", "seal", "validate", "package"} {
		if err := execute([]string{name, "--help"}); err != nil {
			t.Errorf("%s help: %v", name, err)
		}
	}
}
