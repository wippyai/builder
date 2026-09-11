// SPDX-License-Identifier: MIT
package assemble

import (
	"strings"
	"testing"
)

func TestExplicitApplicationLaunchComposition(t *testing.T) {
	m := fixture()
	m.Application.Baseline = "embedded"
	m.Native = []Native{{Module: "example.com/host", Version: "v1.0.0", Package: "example.com/host", Factory: "New", Launch: true}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	code, err := Generate(&m, false)
	if err != nil {
		t.Fatal(err)
	}
	source := string(code)
	for _, required := range []string{`application "github.com/wippyai/runtime/cmd/app"`, "component0 := native0.New()", "component0.Launch", `"embedded"`, "[]boot.Component{component0}"} {
		if !strings.Contains(source, required) {
			t.Fatalf("generated composition lacks %q:\n%s", required, source)
		}
	}
	if strings.Count(source, "native0.New()") != 1 {
		t.Fatal("launch and boot must share one constructed host")
	}
	toolchain, err := Generate(&m, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(toolchain), "component0.Launch") || strings.Contains(string(toolchain), "Baseline:") {
		t.Fatal("runtime tooling acquired application launch policy")
	}
}

func TestApplicationPolicyValidation(t *testing.T) {
	m := fixture()
	m.Application.Baseline = "fallback"
	if err := m.Validate(); err == nil {
		t.Fatal("unknown baseline accepted")
	}
	m.Application.Baseline = "activated"
	m.Native = []Native{
		{Module: "example.com/first", Version: "v1.0.0", Package: "example.com/first", Factory: "New", Launch: true},
		{Module: "example.com/second", Version: "v1.0.0", Package: "example.com/second", Factory: "New", Launch: true},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("multiple launch owners accepted")
	}
}

func TestDefaultAndLegacyGenerationDoNotSelectLaunchPolicy(t *testing.T) {
	m := fixture()
	for _, path := range []string{"github.com/wippyai/runtime/cmd/app", "github.com/wippyai/runtime/application"} {
		code, err := generate(&m, false, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(code), path) || strings.Contains(string(code), "Launch:") || strings.Contains(string(code), "Baseline:") {
			t.Fatalf("unexpected default composition:\n%s", code)
		}
	}
}
