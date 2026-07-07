package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFloorSpelling(t *testing.T) {
	caret, _ := parseConstraint("^1.2")
	f, ok := newFloorSpelling(caret)
	if !ok || f(mustSemver(t, "v1.5.2")) != "^1.5.2" {
		t.Errorf("caret upgrade spelling = %v/%q; want ^1.5.2", ok, f(mustSemver(t, "v1.5.2")))
	}
	tilde, _ := parseConstraint("~1.3.2")
	g, ok := newFloorSpelling(tilde)
	if !ok || g(mustSemver(t, "v1.3.9")) != "~1.3.9" {
		t.Errorf("tilde upgrade spelling wrong: %q", g(mustSemver(t, "v1.3.9")))
	}
	// Exact / wildcard have no raisable floor.
	for _, s := range []string{"v1.0.0", "=v1.0.0", "1.3.*"} {
		c, _ := parseConstraint(s)
		if _, ok := newFloorSpelling(c); ok {
			t.Errorf("%q should have no upgrade floor", s)
		}
	}
}

func TestRewriteDepVersion(t *testing.T) {
	dir := t.TempDir()
	body := "[package]\nname = \"app\"\n\n[dep.kv]\nsource  = \"github.com/x/kv\"\nversion = \"^1.2\"   # a comment\nkind    = \"aril\"\n"
	writeFile(t, dir, "aril.toml", body)
	p := filepath.Join(dir, "aril.toml")
	if err := rewriteDepVersion(p, "kv", "^1.5.2"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	out, _ := os.ReadFile(p)
	s := string(out)
	if !strings.Contains(s, "version = \"^1.5.2\"") {
		t.Errorf("version not rewritten:\n%s", s)
	}
	// Other lines preserved (source, kind, the [package] name).
	if !strings.Contains(s, "source  = \"github.com/x/kv\"") || !strings.Contains(s, "kind    = \"aril\"") || !strings.Contains(s, "name = \"app\"") {
		t.Errorf("rewrite disturbed other lines:\n%s", s)
	}
	// The rewritten manifest still parses to the raised floor.
	m, err := parseProjectManifest(p)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if m.deps[0].version != "^1.5.2" {
		t.Errorf("reparsed version = %q; want ^1.5.2", m.deps[0].version)
	}
}

func TestUpgradeRaisesFloorAndRelocks(t *testing.T) {
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	repo := filepath.Join(root, "kv-repo")
	gitInit(t, repo, map[string]string{
		"aril.toml": "[package]\nname = \"kv\"\n",
		"kv.aril":   "func Get(): int {\n  return 1\n}\n",
	}, "v1.2.0")
	tagAt(t, repo, "v1.5.2")
	tagAt(t, repo, "v2.0.0") // out of the ^1.2 window

	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[package]\nname = \"app\"\n[dep.kv]\nsource = \""+repo+"\"\nversion = \"^1.2\"\n")

	// Drive `aril upgrade` from the app dir.
	restore := chdir(t, app)
	defer restore()
	if code := cmdUpgrade(nil); code != 0 {
		t.Fatalf("aril upgrade exited %d", code)
	}

	// The manifest floor is raised to the highest in-window tag (v1.5.2, not v2.0.0).
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	if m.deps[0].version != "^1.5.2" {
		t.Errorf("floor after upgrade = %q; want ^1.5.2", m.deps[0].version)
	}
	// The lock records the newly-selected v1.5.2.
	lock, err := readLock(app)
	if err != nil || len(lock) != 1 || lock[0].version != "v1.5.2" {
		t.Fatalf("lock after upgrade = %+v (err %v); want kv@v1.5.2", lock, err)
	}
}

// chdir switches the working directory for a test and returns a restore func.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { os.Chdir(prev) }
}
