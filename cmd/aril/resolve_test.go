package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Coverage for external-module resolution (RFC-0008 §Resolution): a declared
// [dep.<name>] import resolves into that module's package tree, its
// source joins the build, and its own imports resolve against its manifest.

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func TestClassifyImportExternal(t *testing.T) {
	m := &projectManifest{
		dir:  "/proj",
		name: "myapp",
		deps: []dependency{{name: "kv", replace: "../aril-kv", kind: "aril"}},
	}
	kind, target := classifyImport("kv/store", m, nil)
	if kind != importExternal {
		t.Fatalf("classifyImport(kv/store) kind = %v; want importExternal", kind)
	}
	// replace is relative to the manifest dir; the package is the remaining path.
	want := filepath.Join("/proj", "..", "aril-kv", "store")
	if target != want {
		t.Errorf("target = %q; want %q", target, want)
	}
	// bare `import kv` resolves to the module root.
	if _, root := classifyImport("kv", m, nil); root != filepath.Join("/proj", "..", "aril-kv") {
		t.Errorf("bare-root target = %q", root)
	}
	// a non-dep path is unaffected.
	if kind, _ := classifyImport("totally/unknown", m, nil); kind != importUnknown {
		t.Errorf("unknown path misclassified as %v", kind)
	}
}

// buildTwoModuleProject writes an `app` project depending (via replace) on a
// sibling `lib` aril library exposing package `greet`. Returns the app dir.
func buildTwoModuleProject(t *testing.T, root string) string {
	t.Helper()
	app := filepath.Join(root, "app")
	greet := filepath.Join(root, "lib", "greet")
	mkdirAll(t, app)
	mkdirAll(t, greet)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nreplace = \"../lib\"\nkind = \"aril\"\n")
	writeFile(t, app, "main.aril", "import lib/greet\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "lib"), "aril.toml", "[project]\nname = \"lib\"\n")
	writeFile(t, greet, "greet.aril", "func Hello(): string {\n  return \"hi\"\n}\n")
	return app
}

func TestResolveExternalArilDep(t *testing.T) {
	root := t.TempDir()
	app := buildTwoModuleProject(t, root)
	m, err := parseProjectManifest(filepath.Join(app, "aril.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	found := false
	for _, f := range res.files {
		if strings.HasSuffix(f, "greet.aril") {
			found = true
		}
	}
	if !found {
		t.Errorf("external dep source not pulled into the build: %v", res.files)
	}
	if !res.userImports["lib/greet"] {
		t.Errorf("external import not marked for Go-import stripping: %v", res.userImports)
	}
}

func TestResolveExternalNotFetched(t *testing.T) {
	// A source/version dep with no replace resolves to the cache; absent there,
	// it is E0121 (not fetched), not E0117.
	root := t.TempDir()
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	t.Setenv("ARIL_CACHE", filepath.Join(root, "empty-cache"))
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.kv]\nsource = \"github.com/x/kv\"\nversion = \"v1.0.0\"\nkind = \"aril\"\n")
	writeFile(t, app, "main.aril", "import kv\n\nfunc main() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0121") {
		t.Fatalf("want E0121 not-fetched, got: %v", err)
	}
}

func TestResolveExternalKindGo(t *testing.T) {
	// A kind="go" dep (RFC-0010): `import pq` pulls the consumer-owned binding
	// table into the build (stripped from the Go import block), and the bound Go
	// module is recorded as a require+replace entry (replace-target = the raw Go
	// module dir here). The table's module path is recovered from the module's
	// own go.mod when `source` is omitted.
	root := t.TempDir()
	app := filepath.Join(root, "app")
	dep := filepath.Join(root, "pq")
	mkdirAll(t, app)
	mkdirAll(t, dep)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.pq]\nreplace = \"../pq\"\nkind = \"go\"\npath = \"table.aril\"\n")
	writeFile(t, app, "main.aril", "import pq\n\nfunc main() {}\n")
	writeFile(t, app, "table.aril", "extern func x(): int @go(\"example.com/pq.X\")\n")
	writeFile(t, dep, "go.mod", "module example.com/pq\n\ngo 1.23\n")
	writeFile(t, dep, "pq.go", "package pq\n\nfunc X() int { return 1 }\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	res, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err != nil {
		t.Fatalf("kind=go resolve failed: %v", err)
	}
	if !res.userImports["pq"] {
		t.Errorf("import pq should be stripped (userImports), got %v", res.userImports)
	}
	if !containsSuffix(res.files, "table.aril") {
		t.Errorf("the binding table should join the build files, got %v", res.files)
	}
	if len(res.goDeps) != 1 || res.goDeps[0].Module != "example.com/pq" {
		t.Fatalf("want one goDep for example.com/pq, got %v", res.goDeps)
	}
	if fi, err := os.Stat(res.goDeps[0].Vendor); err != nil || !fi.IsDir() {
		t.Errorf("replace target should be the raw Go module dir, got %q (%v)", res.goDeps[0].Vendor, err)
	}
}

func TestResolveExternalKindGoMissingTable(t *testing.T) {
	// A kind="go" dep whose declared table file is absent is E0121 (the table is
	// the binding surface — a missing one cannot build), never a silent miss.
	root := t.TempDir()
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.pq]\nreplace = \"..\"\nkind = \"go\"\npath = \"nope.aril\"\n")
	writeFile(t, app, "main.aril", "import pq\n\nfunc main() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0121") {
		t.Fatalf("want E0121 for a missing binding table, got: %v", err)
	}
}

func containsSuffix(xs []string, suffix string) bool {
	for _, x := range xs {
		if strings.HasSuffix(x, suffix) {
			return true
		}
	}
	return false
}

func TestResolveExternalModuleWithoutManifest(t *testing.T) {
	// The external module root has no aril.toml (even though an ancestor dir
	// does) — it must be E0121 (not fetched / no module), never silently bound
	// to the ancestor's manifest.
	root := t.TempDir()
	writeFile(t, root, "aril.toml", "[project]\nname = \"workspace\"\n") // ancestor manifest
	app := filepath.Join(root, "app")
	greet := filepath.Join(root, "lib", "greet")
	mkdirAll(t, app)
	mkdirAll(t, greet)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nreplace = \"../lib\"\nkind = \"aril\"\n")
	writeFile(t, app, "main.aril", "import lib/greet\n\nfunc main() {}\n")
	// ../lib deliberately has NO aril.toml of its own.
	writeFile(t, greet, "greet.aril", "func Hello(): string {\n  return \"hi\"\n}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0121") {
		t.Fatalf("a module without its own aril.toml must be E0121, got: %v", err)
	}
}

func TestResolveExternalMissingSubPackage(t *testing.T) {
	// The module is present (has aril.toml), but the imported sub-package does
	// not exist → E0117 (an unknown path within a present module), not E0121
	// ("run aril get" cannot fix a typo'd package name).
	root := t.TempDir()
	app := buildTwoModuleProject(t, root) // provides lib with package greet
	writeFile(t, app, "main.aril", "import lib/nope\n\nfunc main() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0117") {
		t.Fatalf("a missing sub-package of a present module must be E0117, got: %v", err)
	}
}

func TestResolveCrossModuleCycle(t *testing.T) {
	// app depends on lib and lib depends back on app (each via replace); the
	// import graph is cyclic across module boundaries → E0116 (D20).
	root := t.TempDir()
	app := filepath.Join(root, "app")
	lib := filepath.Join(root, "lib")
	mkdirAll(t, app)
	mkdirAll(t, lib)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nreplace = \"../lib\"\nkind = \"aril\"\n")
	writeFile(t, app, "main.aril", "import lib\n\nfunc main() {}\n")
	writeFile(t, lib, "aril.toml", "[project]\nname = \"lib\"\n[dep.app]\nreplace = \"../app\"\nkind = \"aril\"\n")
	writeFile(t, lib, "lib.aril", "import app\n\nfunc helper() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0116") {
		t.Fatalf("want E0116 cyclic import across modules, got: %v", err)
	}
}

func TestResolveKindGoBindingUniqueness(t *testing.T) {
	// Two different kind="go" deps binding the SAME Go module is E0124 (a Go
	// module may be bound by at most one table — duplicate extern decls otherwise).
	root := t.TempDir()
	app := filepath.Join(root, "app")
	raw := filepath.Join(root, "raw")
	mkdirAll(t, app)
	mkdirAll(t, raw)
	writeFile(t, raw, "go.mod", "module example.com/dup\n\ngo 1.22\n")
	writeFile(t, raw, "d.go", "package dup\n\nfunc X() int { return 1 }\n")
	writeFile(t, app, "aril.toml",
		"[package]\nname = \"app\"\n"+
			"[dep.a]\nreplace = \"../raw\"\nkind = \"go\"\npath = \"ta.aril\"\n"+
			"[dep.b]\nreplace = \"../raw\"\nkind = \"go\"\npath = \"tb.aril\"\n")
	writeFile(t, app, "ta.aril", "extern func xa(): int @go(\"example.com/dup.X\")\n")
	writeFile(t, app, "tb.aril", "extern func xb(): int @go(\"example.com/dup.X\")\n")
	writeFile(t, app, "main.aril", "import a\nimport b\n\nfunc main() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "E0124") {
		t.Fatalf("want E0124 for two deps binding one Go module, got: %v", err)
	}
}
