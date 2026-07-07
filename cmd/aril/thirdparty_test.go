package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigReaderThirdParty — the headline result: a Aril program that
// binds a *third-party* Go module (example.com/arilkv, vendored) builds
// hermetically (manifest-driven require + replace) and runs.
func TestConfigReaderThirdParty(t *testing.T) {
	stdout, stderr, exit := runAril(t, "run", "examples/ffi/config_reader/config_reader.aril")
	if exit != 0 {
		t.Fatalf("config_reader.aril exited %d (stderr: %s)", exit, stderr)
	}
	if stdout != "aril\ngo\n3\n" {
		t.Errorf("config_reader.aril stdout = %q; want \"aril\\ngo\\n3\\n\"", stdout)
	}
}

// TestManifestRoundTrip — the manifest loads and lists the vendored dep.
func TestManifestRoundTrip(t *testing.T) {
	root := projectRoot(t)
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.ThirdParty) == 0 {
		t.Fatal("manifest has no third-party deps")
	}
	var found bool
	for _, d := range m.ThirdParty {
		if d.ImportPath == "example.com/arilkv" {
			found = true
			if d.Module == "" || d.Version == "" || d.Vendor == "" {
				t.Errorf("arilkv dep incomplete: %+v", d)
			}
		}
	}
	if !found {
		t.Error("manifest missing example.com/arilkv")
	}
}

// TestManifestAbsentIsEmpty — a missing manifest (root "", or a dir with
// no std/bindings.json) degrades to an empty manifest, not an error:
// third-party binding is simply unavailable, stdlib-only still builds.
func TestManifestAbsentIsEmpty(t *testing.T) {
	if m, err := loadManifest(""); err != nil || len(m.ThirdParty) != 0 {
		t.Errorf("loadManifest(\"\") = (%+v, %v); want empty, nil", m, err)
	}
	if m, err := loadManifest(t.TempDir()); err != nil || len(m.ThirdParty) != 0 {
		t.Errorf("loadManifest(empty dir) = (%+v, %v); want empty, nil", m, err)
	}
}

// TestManifestMalformedErrors — a corrupt manifest is a clear error
// (fails closed), never a silent empty.
func TestManifestMalformedErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "std"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "std", "bindings.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(dir); err == nil {
		t.Error("malformed manifest should error, not silently succeed")
	}
}

// TestUsedThirdParty — only deps the emitted Go actually imports are
// selected (so a stdlib-only program drags in no require).
func TestUsedThirdParty(t *testing.T) {
	m := manifest{ThirdParty: []thirdPartyDep{
		{ImportPath: "example.com/arilkv", Module: "example.com/arilkv", Version: "v0.0.0", Vendor: "std/vendor/arilkv"},
	}}
	withImport := "package main\nimport \"example.com/arilkv\"\n"
	if got := usedThirdParty(withImport, m); len(got) != 1 {
		t.Errorf("expected 1 used dep, got %d", len(got))
	}
	stdlibOnly := "package main\nimport \"regexp\"\n"
	if got := usedThirdParty(stdlibOnly, m); len(got) != 0 {
		t.Errorf("stdlib-only program pulled %d third-party deps", len(got))
	}
}

// TestGoModText — the emitted go.mod gains require + an absolute,
// hermetic replace for a used dep, and stays require-free otherwise.
func TestGoModText(t *testing.T) {
	dep := thirdPartyDep{ImportPath: "example.com/arilkv", Module: "example.com/arilkv", Version: "v0.0.0", Vendor: "std/vendor/arilkv"}

	bare := goModText("/repo", nil, "")
	if strings.Contains(bare, "require") || strings.Contains(bare, "replace") {
		t.Errorf("stdlib-only go.mod should be require-free:\n%s", bare)
	}
	if !strings.Contains(bare, "\ngo "+defaultGoVersion+"\n") {
		t.Errorf("go.mod should carry the default Go floor %q:\n%s", defaultGoVersion, bare)
	}

	withDep := goModText("/repo", []thirdPartyDep{dep}, "")
	for _, want := range []string{
		"require example.com/arilkv v0.0.0",
		"replace example.com/arilkv => /repo/std/vendor/arilkv",
	} {
		if !strings.Contains(withDep, want) {
			t.Errorf("go.mod missing %q:\n%s", want, withDep)
		}
	}
}

// TestMaxGoVersion — the emitted Go floor is the max of the default floor, the
// root [toolchain] go, and each Go-binding dep's own go.mod `go` directive
// (RFC-0008 §Compatibility axes — one Go module, the root picks max-of-floors).
func TestMaxGoVersion(t *testing.T) {
	if got := maxGoVersion(nil, nil); got != defaultGoVersion {
		t.Errorf("no floors → default %q, got %q", defaultGoVersion, got)
	}
	if got := maxGoVersion(&projectManifest{toolchainGo: "1.21"}, nil); got != defaultGoVersion {
		t.Errorf("a below-default root floor must not lower it: got %q", got)
	}
	if got := maxGoVersion(&projectManifest{toolchainGo: "1.24"}, nil); got != "1.24" {
		t.Errorf("root [toolchain] go should raise the floor: got %q", got)
	}
	// A dep whose go.mod declares a higher floor raises the emitted version.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := maxGoVersion(nil, []thirdPartyDep{{Vendor: dir}}); got != "1.23" {
		t.Errorf("dep go directive should raise the floor: got %q", got)
	}
}

func TestGoVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.22", "1.23", true},
		{"1.23", "1.22", false},
		{"1.22", "1.22", false},
		{"1.22", "1.22.1", true},
		{"1.22.1", "1.22", false},
		{"1.9", "1.22", true}, // numeric, not lexical
	}
	for _, c := range cases {
		if got := goVersionLess(c.a, c.b); got != c.want {
			t.Errorf("goVersionLess(%q,%q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}
