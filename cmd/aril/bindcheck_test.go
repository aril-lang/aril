package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/bindgen"
)

// moduleLoadingAvailable reports whether the source importer can load a trivial
// local module in this environment (no GOROOT src → false; the E0126 check can
// only fire when the module loads — sound over red).
func moduleLoadingAvailable(t *testing.T) bool {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/probe\n\ngo 1.22\n")
	writeFile(t, dir, "p.go", "package probe\n\nfunc F() {}\n")
	_, err := bindgen.LoadModulePackage("example.com/probe", dir, "example.com/probe", "")
	return err == nil
}

// setupGoBindingProject lays out a raw Go module + a consumer project with a
// kind="go" dep (via replace) whose table contains the given extern lines.
func setupGoBindingProject(t *testing.T, goSrc string, tableExterns string) *projectManifest {
	t.Helper()
	root := t.TempDir()
	raw := filepath.Join(root, "raw")
	app := filepath.Join(root, "app")
	mkdirAll(t, raw)
	mkdirAll(t, app)
	writeFile(t, raw, "go.mod", "module example.com/lib\n\ngo 1.22\n")
	writeFile(t, raw, "lib.go", goSrc)
	writeFile(t, app, "aril.toml",
		"[package]\nname = \"app\"\n[dep.lib]\nreplace = \"../raw\"\nkind = \"go\"\npath = \"table.aril\"\n")
	writeFile(t, app, "table.aril", tableExterns)
	m, err := parseProjectManifest(filepath.Join(app, "aril.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestValidateGoBindingTableDrift — a table binding a symbol the module does not
// export is E0126.
func TestValidateGoBindingTableDrift(t *testing.T) {
	m := setupGoBindingProject(t,
		"package lib\n\nfunc Present() int { return 1 }\n",
		"extern func present(): int @go(\"example.com/lib.Present\")\n"+
			"extern func gone(): int @go(\"example.com/lib.Removed\")\n")
	if !moduleLoadingAvailable(t) {
		t.Skip("module loading unavailable in this environment (no GOROOT source tree)")
	}
	err := validateGoBindingTables(m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0126") || !strings.Contains(err.Error(), "Removed") {
		t.Fatalf("want E0126 naming the absent symbol, got: %v", err)
	}
}

// TestValidateGoBindingTableClean — a table whose every symbol exists validates.
func TestValidateGoBindingTableClean(t *testing.T) {
	if !moduleLoadingAvailable(t) {
		// Guard symmetrically with the drift test: without loading, validation
		// skips every package and returns nil vacuously — a pass that proves
		// nothing (dev-insights §6). Only assert clean when loading genuinely ran.
		t.Skip("module loading unavailable in this environment (no GOROOT source tree)")
	}
	m := setupGoBindingProject(t,
		"package lib\n\nfunc A() int { return 1 }\n\nfunc B() string { return \"\" }\n",
		"extern func a(): int @go(\"example.com/lib.A\")\n"+
			"extern func b(): string @go(\"example.com/lib.B\")\n")
	if err := validateGoBindingTables(m, nil); err != nil {
		t.Fatalf("clean table should validate, got: %v", err)
	}
}
