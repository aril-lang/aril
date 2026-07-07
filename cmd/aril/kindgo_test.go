package main

import (
	"os"
	"path/filepath"
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
