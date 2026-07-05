package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit makes dir a git repo with the given files committed, then tags the
// commit. Hermetic — no network; `aril get` clones from this local path.
func gitInit(t *testing.T, dir string, files map[string]string, tag string) {
	t.Helper()
	mkdirAll(t, dir)
	for name, src := range files {
		p := filepath.Join(dir, name)
		mkdirAll(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := runGit(dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := runGit(dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := runGit(dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if tag != "" {
		if err := runGit(dir, "tag", tag); err != nil {
			t.Fatalf("git tag: %v", err)
		}
	}
}

func TestLockRoundtrip(t *testing.T) {
	dir := t.TempDir()
	in := []lockEntry{
		{name: "b", source: "github.com/x/b", version: "v2.0.0", resolved: "abc", hash: "h2"},
		{name: "a", source: "github.com/x/a", version: "v1.0.0", resolved: "def", hash: "h1"},
	}
	if err := writeLock(dir, in); err != nil {
		t.Fatalf("writeLock: %v", err)
	}
	out, err := readLock(dir)
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if len(out) != 2 || out[0].name != "a" || out[1].name != "b" { // sorted by name
		t.Fatalf("roundtrip = %+v", out)
	}
	if out[0].source != "github.com/x/a" || out[0].version != "v1.0.0" || out[0].resolved != "def" || out[0].hash != "h1" {
		t.Errorf("entry a not preserved: %+v", out[0])
	}
}

func TestFetchAndResolveOffline(t *testing.T) {
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		if _, err := runGitVersion(); err != nil {
			t.Skip("git not available")
		}
	}
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	t.Setenv("ARIL_CACHE", cache)

	// A published library, as a local git repo tagged v1.0.0.
	repo := filepath.Join(root, "greetlib-repo")
	gitInit(t, repo, map[string]string{
		"aril.toml":  "[project]\nname = \"greetlib\"\n",
		"greet.aril": "func Hi(): string {\n  return \"hi\"\n}\n",
	}, "v1.0.0")

	// A consumer declaring it as a source/version dep (no replace → fetched).
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.greetlib]\nsource = \""+repo+"\"\nversion = \"v1.0.0\"\nkind = \"aril\"\n")
	writeFile(t, app, "main.aril", "import greetlib\n\nfunc main() {}\n")

	m, err := parseProjectManifest(filepath.Join(app, "aril.toml"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fetchAll(m)
	if err != nil {
		t.Fatalf("fetchAll: %v", err)
	}
	if len(entries) != 1 || entries[0].name != "greetlib" || entries[0].resolved == "" || entries[0].hash == "" {
		t.Fatalf("lock entry = %+v", entries)
	}
	// The module is now in the cache (offline-resolvable).
	if _, err := os.Stat(filepath.Join(cacheModuleDir(repo, "v1.0.0"), "greet.aril")); err != nil {
		t.Fatalf("module not cached: %v", err)
	}
	// The resolver finds it offline — greet.aril joins the build.
	res, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m)
	if err != nil {
		t.Fatalf("offline resolve after get: %v", err)
	}
	found := false
	for _, f := range res.files {
		if strings.HasSuffix(f, "greet.aril") {
			found = true
		}
	}
	if !found {
		t.Errorf("fetched module source not resolved offline: %v", res.files)
	}
}

func TestFetchVersionConflict(t *testing.T) {
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))

	// liba tagged at two versions.
	liba := filepath.Join(root, "liba")
	gitInit(t, liba, map[string]string{
		"aril.toml": "[project]\nname = \"liba\"\n",
		"a.aril":    "func A(): int {\n  return 1\n}\n",
	}, "v1.0.0")
	if err := runGit(liba, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "v2"); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if err := runGit(liba, "tag", "v2.0.0"); err != nil {
		t.Fatalf("tag v2: %v", err)
	}

	// libb depends on liba@v2.0.0.
	libb := filepath.Join(root, "libb")
	gitInit(t, libb, map[string]string{
		"aril.toml": "[project]\nname = \"libb\"\n[dep.liba]\nsource = \"" + liba + "\"\nversion = \"v2.0.0\"\nkind = \"aril\"\n",
		"b.aril":    "func B(): int {\n  return 2\n}\n",
	}, "v1.0.0")

	// root depends on liba@v1.0.0 AND on libb (which pulls liba@v2.0.0) → conflict.
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml",
		"[project]\nname = \"app\"\n"+
			"[dep.liba]\nsource = \""+liba+"\"\nversion = \"v1.0.0\"\nkind = \"aril\"\n"+
			"[dep.libb]\nsource = \""+libb+"\"\nversion = \"v1.0.0\"\nkind = \"aril\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	_, err := fetchAll(m)
	if err == nil || !strings.Contains(err.Error(), "E0122") {
		t.Fatalf("want E0122 version conflict, got: %v", err)
	}
}

func TestLockRoundtripBackslashSource(t *testing.T) {
	// A local/Windows source path with backslashes must survive the write/read
	// roundtrip (writer and reader agree on literal, non-escaped quoting).
	dir := t.TempDir()
	in := []lockEntry{{name: "a", source: `C:\pkg\lib`, version: "v1.0.0", resolved: "abc", hash: "h"}}
	if err := writeLock(dir, in); err != nil {
		t.Fatalf("writeLock: %v", err)
	}
	out, err := readLock(dir)
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if len(out) != 1 || out[0].source != `C:\pkg\lib` {
		t.Errorf("backslash source not preserved: %+v", out)
	}
}

func TestGetPreservesResolvedOnReRun(t *testing.T) {
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	repo := filepath.Join(root, "lib-repo")
	gitInit(t, repo, map[string]string{"aril.toml": "[project]\nname = \"lib\"\n", "x.aril": "func X(): int {\n  return 1\n}\n"}, "v1.0.0")
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nsource = \""+repo+"\"\nversion = \"v1.0.0\"\nkind = \"aril\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))

	first, err := fetchAll(m)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if err := writeLock(m.dir, first); err != nil {
		t.Fatal(err)
	}
	// A second `get` hits the immutable cache; it must NOT blank `resolved`.
	second, err := fetchAll(m)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if len(second) != 1 || second[0].resolved == "" || second[0].resolved != first[0].resolved {
		t.Errorf("re-get lost the resolved pin: first=%q second=%q", first[0].resolved, second[0].resolved)
	}
}

func TestGetRejectsBranchAsVersion(t *testing.T) {
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	repo := filepath.Join(root, "lib-repo")
	gitInit(t, repo, map[string]string{"aril.toml": "[project]\nname = \"lib\"\n", "x.aril": "func X(): int {\n  return 1\n}\n"}, "v1.0.0")
	if err := runGit(repo, "branch", "feature-x"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	// A branch name is not an exact pin — must be rejected.
	writeFile(t, app, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nsource = \""+repo+"\"\nversion = \"feature-x\"\nkind = \"aril\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	if _, err := fetchAll(m); err == nil || !strings.Contains(err.Error(), "exact pin") {
		t.Fatalf("want exact-pin rejection of a branch, got: %v", err)
	}
}

func runGitVersion() (string, error) { return gitOutput("", "version") }
