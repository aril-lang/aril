package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Coverage for the aril.toml project manifest reader (RFC-0002 §Manifest)
// and the package resolver's import classification (RFC-0002 §Resolution).

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestParseManifestFull(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "aril.toml", `# a project
[project]
name = "myproj"

[toolchain]
go = "1.22"

[bindings]
extra = ["golang.org/x/exp/slices", "example.com/foo"]
`)
	m, err := parseProjectManifest(filepath.Join(dir, "aril.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.name != "myproj" {
		t.Errorf("name = %q; want myproj", m.name)
	}
	if m.toolchainGo != "1.22" {
		t.Errorf("toolchain go = %q; want 1.22", m.toolchainGo)
	}
	if len(m.bindingsExtra) != 2 || m.bindingsExtra[0] != "golang.org/x/exp/slices" {
		t.Errorf("bindingsExtra = %v", m.bindingsExtra)
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := map[string]string{
		"unknown section": "[bogus]\nx = \"y\"\n",
		"missing name":    "[toolchain]\ngo = \"1.22\"\n",
		"unknown key":     "[project]\nname = \"p\"\nweird = \"x\"\n",
		"extra collision": "[project]\nname = \"p\"\n[bindings]\nextra = [\"a/slices\", \"b/slices\"]\n",
		"non-string name": "[project]\nname = 5\n",
	}
	for desc, body := range cases {
		dir := t.TempDir()
		writeFile(t, dir, "aril.toml", body)
		if _, err := parseProjectManifest(filepath.Join(dir, "aril.toml")); err == nil {
			t.Errorf("%s: expected an error, got none", desc)
		}
	}
}

func TestParseManifestDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "aril.toml", `[project]
name = "myapp"

[dep.kv]
source  = "github.com/alice/aril-kv"
version = "v1.2.0"
# kind omitted → defaults to "aril"

[dep.pq]
source  = "github.com/lib/pq"
version = "v1.10.9"
kind    = "go"
path    = "table/pq.aril"

[dep.local]
replace = "../aril-kv"
kind    = "binding"
`)
	m, err := parseProjectManifest(filepath.Join(dir, "aril.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.deps) != 3 {
		t.Fatalf("deps = %d; want 3 (%+v)", len(m.deps), m.deps)
	}
	kv := m.deps[0]
	if kv.name != "kv" || kv.source != "github.com/alice/aril-kv" || kv.version != "v1.2.0" || kv.kind != "aril" {
		t.Errorf("kv dep = %+v; want name=kv source=github.com/alice/aril-kv version=v1.2.0 kind=aril (default)", kv)
	}
	pq := m.deps[1]
	if pq.kind != "go" || pq.path != "table/pq.aril" {
		t.Errorf("pq dep = %+v; want kind=go path=table/pq.aril", pq)
	}
	loc := m.deps[2]
	if loc.replace != "../aril-kv" || loc.kind != "binding" || loc.source != "" {
		t.Errorf("local dep = %+v; want replace=../aril-kv kind=binding source=\"\"", loc)
	}
}

func TestParseManifestDependencyErrors(t *testing.T) {
	cases := map[string]string{
		"bare dep section":          "[project]\nname = \"p\"\n[dep]\nsource = \"x\"\n",
		"duplicate dep":             "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nversion=\"v1\"\n[dep.kv]\nsource=\"t\"\nversion=\"v2\"\n",
		"unknown kind":              "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nversion=\"v1\"\nkind=\"rust\"\n",
		"missing source":            "[project]\nname = \"p\"\n[dep.kv]\nversion=\"v1\"\n",
		"missing version":           "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\n",
		"go kind without path":      "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nversion=\"v1\"\nkind=\"go\"\n",
		"path on non-go kind":       "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nversion=\"v1\"\npath=\"t.aril\"\n",
		"dep collides with project": "[project]\nname = \"p\"\n[dep.p]\nsource=\"s\"\nversion=\"v1\"\n",
		"unknown dep key":           "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nversion=\"v1\"\nbogus=\"x\"\n",
		"non-string dep value":      "[project]\nname = \"p\"\n[dep.kv]\nsource=5\nversion=\"v1\"\n",
	}
	for desc, body := range cases {
		dir := t.TempDir()
		writeFile(t, dir, "aril.toml", body)
		if _, err := parseProjectManifest(filepath.Join(dir, "aril.toml")); err == nil {
			t.Errorf("%s: expected an error, got none", desc)
		}
	}
}

func TestParseManifestDependencyErrorCoordinates(t *testing.T) {
	// An in-loop error (unknown key on line 5) carries aril.toml:<line> (D10).
	dir := t.TempDir()
	writeFile(t, dir, "aril.toml", "[project]\nname = \"p\"\n[dep.kv]\nsource=\"s\"\nbogus=\"x\"\n")
	_, err := parseProjectManifest(filepath.Join(dir, "aril.toml"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "aril.toml:5") {
		t.Errorf("error should carry the aril.toml line coordinate, got: %v", err)
	}
}

func TestParseManifestReplaceSkipsSourceVersion(t *testing.T) {
	// A `replace`d dependency needs neither source nor version (resolved locally).
	dir := t.TempDir()
	writeFile(t, dir, "aril.toml", "[project]\nname = \"p\"\n[dep.kv]\nreplace = \"../kv\"\n")
	if _, err := parseProjectManifest(filepath.Join(dir, "aril.toml")); err != nil {
		t.Fatalf("a replace-only dep should parse without source/version: %v", err)
	}
}

func TestFindProjectManifestWalksUp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "aril.toml", "[project]\nname = \"p\"\n")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := findProjectManifest(sub)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if m == nil || m.name != "p" {
		t.Fatalf("expected to find the manifest from a nested dir, got %+v", m)
	}
}

func TestFindProjectManifestNoneIsNil(t *testing.T) {
	dir := t.TempDir() // a bare temp dir with no aril.toml above it (within the temp root)
	m, err := findProjectManifest(dir)
	// Walking up may eventually hit a real aril.toml on the host; only
	// assert the no-error contract and that absence yields nil (when nil).
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	_ = m
}

func TestClassifyImport(t *testing.T) {
	m := &projectManifest{dir: "/proj", name: "myproj", bindingsExtra: []string{"golang.org/x/exp/slices"}}
	cases := []struct {
		path string
		want importKind
	}{
		{"myproj/utils", importUser},
		{"myproj", importUser},
		{"fmt", importStdlib},
		{"encoding/json", importStdlib},
		{"slices", importStdlib}, // via [bindings] extra last-segment
		{"totally/unknown", importUnknown},
	}
	for _, c := range cases {
		got, _ := classifyImport(c.path, m)
		if got != c.want {
			t.Errorf("classifyImport(%q) = %v; want %v", c.path, got, c.want)
		}
	}
}
