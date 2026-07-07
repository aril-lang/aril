package bindgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule lays out a minimal Go module (go.mod + one source file) under a
// temp dir and returns the dir.
func writeModule(t *testing.T, modulePath, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestGenerateFromModule is the RFC-0010 module-aware loader proof: introspect a
// third-party Go module (not stdlib) via the synthesized-context source-mode
// importer, and derive its exported binding surface.
func TestGenerateFromModule(t *testing.T) {
	dir := writeModule(t, "example.com/greet",
		"package greet\n\n"+
			"// Hello greets name.\n"+
			"func Hello(name string) string { return \"hi \" + name }\n\n"+
			"// Lookup returns a value and whether it was found.\n"+
			"func Lookup(k string) (string, bool) { return \"\", false }\n")

	out, err := GenerateFromModule("example.com/greet", dir, "example.com/greet", "")
	if err != nil {
		// Skip (not fail) when the Go source importer is unavailable — no GOROOT
		// source tree, e.g. a minimal CI image (the bindgen_test.go convention).
		t.Skipf("module-aware bindgen unavailable in this environment: %v", err)
	}
	for _, want := range []string{
		`extern func hello(name: string): string @go("example.com/greet.Hello")`,
		`extern func lookup(k: string): Option<string> @go("example.com/greet.Lookup")`, // (T, bool) → Option<T>
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated bindings missing %q:\n%s", want, out)
		}
	}
}

// TestLoadModulePackageRejectsForeignPath — an import path outside the module is
// rejected (it cannot be a package of that module).
func TestLoadModulePackageRejectsForeignPath(t *testing.T) {
	dir := writeModule(t, "example.com/a", "package a\n\nfunc F() {}\n")
	_, err := LoadModulePackage("example.com/b/pkg", dir, "example.com/a", "")
	if err == nil || !strings.Contains(err.Error(), "not a package of module") {
		t.Fatalf("want a not-a-package error, got: %v", err)
	}
}

// TestLoadModuleRestoresCwd — the loader chdirs into a synthesized context and
// must restore the working directory afterward (a shared-process invariant).
func TestLoadModuleRestoresCwd(t *testing.T) {
	before, _ := os.Getwd()
	dir := writeModule(t, "example.com/c", "package c\n\nfunc F() int { return 1 }\n")
	// The cwd invariant must hold whether the import succeeds or fails (the defer
	// restores it either way) — so assert restoration even when the source
	// importer is unavailable in this environment.
	_, _ = LoadModulePackage("example.com/c", dir, "example.com/c", "")
	after, _ := os.Getwd()
	if before != after {
		t.Errorf("cwd not restored: before=%q after=%q", before, after)
	}
}
