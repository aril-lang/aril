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

func TestResolveExternalKindGoDeferred(t *testing.T) {
	// kind=go/binding external deps are later work; resolving one is a clear
	// E0121 (only kind=aril wired), never a silent miscompile.
	root := t.TempDir()
	app := filepath.Join(root, "app")
	dep := filepath.Join(root, "pq")
	mkdirAll(t, app)
	mkdirAll(t, dep)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.pq]\nreplace = \"../pq\"\nkind = \"go\"\npath = \"t.aril\"\n")
	writeFile(t, app, "main.aril", "import pq\n\nfunc main() {}\n")
	writeFile(t, dep, "aril.toml", "[project]\nname = \"pq\"\n")
	writeFile(t, dep, "pq.aril", "func X() {}\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("want a kind-not-wired error, got: %v", err)
	}
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
