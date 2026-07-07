package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKindGoDependencyBuildsAndRuns is the RFC-0010 kind="go" end-to-end proof:
// an Aril program depends on a raw Go module (no aril.toml) through a
// consumer-owned binding table, and `aril run` compiles the table into the build
// + wires the Go module via require+replace → runs the real Go call. Hermetic: a
// local raw Go module reached by `replace`, no network, no fetch.
func TestKindGoDependencyBuildsAndRuns(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "rawgo")
	app := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(app, "bindings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A raw Go module — the dependency, with no aril.toml. Its `go` directive is
	// 1.22 (Aril's floor, D36) so the emitted go.mod needs no newer toolchain.
	write(filepath.Join(raw, "go.mod"), "module example.com/greet\n\ngo 1.22\n")
	write(filepath.Join(raw, "greet.go"),
		"package greet\n\nfunc Hello(name string) string { return \"Hello, \" + name + \"!\" }\n")

	// The consumer project: a kind="go" dep bound through a consumer-owned table.
	write(filepath.Join(app, "aril.toml"),
		"[package]\nname = \"app\"\n\n[dep.greet]\nreplace = \"../rawgo\"\nkind = \"go\"\npath = \"bindings/greet.aril\"\n")
	write(filepath.Join(app, "bindings", "greet.aril"),
		"extern func hello(name: string): string @go(\"example.com/greet.Hello\")\n")
	write(filepath.Join(app, "main.aril"),
		"import fmt\nimport greet\n\nfunc main() {\n  fmt.println(hello(\"world\"))\n}\n")

	stdout, stderr, exit := runAril(t, "run", filepath.Join(app, "main.aril"))
	if exit != 0 {
		t.Fatalf("kind=go run exited %d\nstderr: %s", exit, stderr)
	}
	if stdout != "Hello, world!\n" {
		t.Errorf("kind=go run stdout = %q; want \"Hello, world!\\n\"", stdout)
	}
}

// TestArilImportFromModule — `aril import --from <module-dir> <path>` introspects
// a local Go module (module-aware, RFC-0010) and prints a curated binding table.
func TestArilImportFromModule(t *testing.T) {
	mod := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(mod, "go.mod"), "module example.com/kv\n\ngo 1.22\n")
	write(filepath.Join(mod, "kv.go"), "package kv\n\nfunc Get(k string) string { return k }\n")

	stdout, stderr, exit := runAril(t, "import", "--from", mod, "example.com/kv")
	if exit != 0 {
		t.Fatalf("aril import --from exited %d\nstderr: %s", exit, stderr)
	}
	want := `extern func get(k: string): string @go("example.com/kv.Get")`
	if !strings.Contains(stdout, want) {
		t.Errorf("aril import --from output missing %q:\n%s", want, stdout)
	}
}

// TestKindBindingBuildsAndRuns — a published binding package (kind="binding",
// RFC-0010): its extern .aril source compiles into the build (kind=aril-style)
// AND the Go module it self-declares (`binds`/`binds-go`) rides require+replace.
// Hermetic: the binding package via `replace`, the bound Go module pre-placed in
// the cache (as `aril get` would fetch it) — no network.
func TestKindBindingBuildsAndRuns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The bound Go module, pre-placed at its cache coordinate.
	boundDir := cacheModuleDir("example.com/lib", "v0.0.1")
	write(filepath.Join(boundDir, "go.mod"), "module example.com/lib\n\ngo 1.22\n")
	write(filepath.Join(boundDir, "lib.go"),
		"package lib\n\nfunc Hello(name string) string { return \"Hello, \" + name + \"!\" }\n")

	// The published binding package (reached via replace).
	bindpkg := filepath.Join(root, "bindpkg")
	write(filepath.Join(bindpkg, "aril.toml"),
		"[package]\nname = \"greet\"\nkind = \"binding\"\nbinds = \"example.com/lib\"\nbinds-go = \"v0.0.1\"\n")
	write(filepath.Join(bindpkg, "greet.aril"),
		"extern func hello(name: string): string @go(\"example.com/lib.Hello\")\n")

	// The consumer.
	app := filepath.Join(root, "app")
	write(filepath.Join(app, "aril.toml"), "[package]\nname = \"app\"\n[dep.greet]\nreplace = \"../bindpkg\"\n")
	write(filepath.Join(app, "main.aril"), "import fmt\nimport greet\n\nfunc main() {\n  fmt.println(hello(\"binding\"))\n}\n")

	stdout, stderr, exit := runAril(t, "run", filepath.Join(app, "main.aril"))
	if exit != 0 {
		t.Fatalf("kind=binding run exited %d\nstderr: %s", exit, stderr)
	}
	if stdout != "Hello, binding!\n" {
		t.Errorf("kind=binding stdout = %q; want \"Hello, binding!\\n\"", stdout)
	}
}
