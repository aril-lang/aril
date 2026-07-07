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
	entries, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("resolveGraph: %v", err)
	}
	if len(entries) != 1 || entries[0].name != "greetlib" || entries[0].resolved == "" || entries[0].hash == "" {
		t.Fatalf("lock entry = %+v", entries)
	}
	// The module is now in the cache (offline-resolvable).
	if _, err := os.Stat(filepath.Join(cacheModuleDir(repo, "v1.0.0"), "greet.aril")); err != nil {
		t.Fatalf("module not cached: %v", err)
	}
	// The resolver finds it offline — greet.aril joins the build.
	res, err := resolvePackages([]string{filepath.Join(app, "main.aril")}, m, nil)
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
	_, err := resolveGraph(m)
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

	first, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if err := writeLock(m.dir, first); err != nil {
		t.Fatal(err)
	}
	// A second `get` hits the immutable cache; it must NOT blank `resolved`.
	second, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if len(second) != 1 || second[0].resolved == "" || second[0].resolved != first[0].resolved {
		t.Errorf("re-get lost the resolved pin: first=%q second=%q", first[0].resolved, second[0].resolved)
	}
}

func TestGetRejectsBranchAsVersion(t *testing.T) {
	// A branch name is neither a range constraint, an exact `vX.Y.Z` tag, nor a
	// commit SHA — so it is rejected at manifest parse (earlier and clearer than
	// the fetch-time "exact pin" check, which still guards a valid-looking ref).
	dir := t.TempDir()
	writeFile(t, dir, "aril.toml", "[project]\nname = \"app\"\n[dep.lib]\nsource = \"github.com/x/lib\"\nversion = \"feature-x\"\nkind = \"aril\"\n")
	_, err := parseProjectManifest(filepath.Join(dir, "aril.toml"))
	if err == nil || !strings.Contains(err.Error(), "constraint") {
		t.Fatalf("want a version-constraint rejection of a branch name, got: %v", err)
	}
}

// tagAt adds a fresh commit + semver tag to an existing local repo.
func tagAt(t *testing.T, repo, tag string) {
	t.Helper()
	if err := runGit(repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", tag); err != nil {
		t.Fatalf("commit %s: %v", tag, err)
	}
	if err := runGit(repo, "tag", tag); err != nil {
		t.Fatalf("tag %s: %v", tag, err)
	}
}

func TestResolveRangedConstraint(t *testing.T) {
	// A caret constraint resolves to the lowest satisfying released tag (MVS),
	// via git-tag enumeration — hermetically, from a local multi-tag repo.
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	repo := filepath.Join(root, "kv-repo")
	gitInit(t, repo, map[string]string{
		"aril.toml": "[package]\nname = \"kv\"\n",
		"kv.aril":   "func Get(): int {\n  return 1\n}\n",
	}, "v1.0.0")
	tagAt(t, repo, "v1.2.0")
	tagAt(t, repo, "v1.3.0")
	tagAt(t, repo, "v2.0.0")

	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[package]\nname = \"app\"\n[dep.kv]\nsource = \""+repo+"\"\nversion = \"^1.2\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	entries, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("resolveGraph: %v", err)
	}
	// ^1.2 admits [1.2.0, 2.0.0); the lowest released tag ≥ floor is v1.2.0.
	if len(entries) != 1 || entries[0].version != "v1.2.0" {
		t.Fatalf("want kv resolved to v1.2.0 (lowest satisfying ^1.2), got %+v", entries)
	}
}

func TestResolveTransitiveMaxOfFloors(t *testing.T) {
	// Two dependencies require a shared module at different floors; MVS selects
	// the maximum — the lowest tag satisfying both (RFC-0008 §Resolution).
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))

	// Shared module `core` with three releases.
	core := filepath.Join(root, "core")
	gitInit(t, core, map[string]string{
		"aril.toml": "[package]\nname = \"core\"\n",
		"c.aril":    "func C(): int {\n  return 0\n}\n",
	}, "v1.0.0")
	tagAt(t, core, "v1.1.0")
	tagAt(t, core, "v1.2.0")

	// liba needs core ^1.0; libb needs core ^1.1.
	liba := filepath.Join(root, "liba")
	gitInit(t, liba, map[string]string{
		"aril.toml": "[package]\nname = \"liba\"\n[dep.core]\nsource = \"" + core + "\"\nversion = \"^1.0\"\n",
		"a.aril":    "func A(): int {\n  return 1\n}\n",
	}, "v1.0.0")
	libb := filepath.Join(root, "libb")
	gitInit(t, libb, map[string]string{
		"aril.toml": "[package]\nname = \"libb\"\n[dep.core]\nsource = \"" + core + "\"\nversion = \"^1.1\"\n",
		"b.aril":    "func B(): int {\n  return 2\n}\n",
	}, "v1.0.0")

	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[package]\nname = \"app\"\n"+
		"[dep.liba]\nsource = \""+liba+"\"\nversion = \"^1.0\"\n"+
		"[dep.libb]\nsource = \""+libb+"\"\nversion = \"^1.0\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	entries, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("resolveGraph: %v", err)
	}
	var coreVer string
	for _, e := range entries {
		if e.name == "core" {
			coreVer = e.version
		}
	}
	// max(^1.0 floor 1.0.0, ^1.1 floor 1.1.0) = 1.1.0 → lowest satisfying = v1.1.0.
	if coreVer != "v1.1.0" {
		t.Fatalf("want core resolved to v1.1.0 (max of floors), got %q (entries %+v)", coreVer, entries)
	}
}

func TestVerifyLockedCacheDetectsTamper(t *testing.T) {
	// A cached module whose content no longer matches aril.lock is E0123.
	if _, err := runGitVersion(); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("ARIL_CACHE", filepath.Join(root, "cache"))
	repo := filepath.Join(root, "lib-repo")
	gitInit(t, repo, map[string]string{
		"aril.toml": "[package]\nname = \"lib\"\n",
		"x.aril":    "func X(): int {\n  return 1\n}\n",
	}, "v1.0.0")
	app := filepath.Join(root, "app")
	mkdirAll(t, app)
	writeFile(t, app, "aril.toml", "[package]\nname = \"app\"\n[dep.lib]\nsource = \""+repo+"\"\nversion = \"v1.0.0\"\n")
	m, _ := parseProjectManifest(filepath.Join(app, "aril.toml"))
	entries, err := resolveGraph(m)
	if err != nil {
		t.Fatalf("resolveGraph: %v", err)
	}
	if err := verifyLockedCache(entries); err != nil {
		t.Fatalf("a fresh cache must verify: %v", err)
	}
	// Tamper the cached tree; the recorded hash no longer matches → E0123.
	writeFile(t, cacheModuleDir(repo, "v1.0.0"), "x.aril", "func X(): int {\n  return 999\n}\n")
	err = verifyLockedCache(entries)
	if err == nil || !strings.Contains(err.Error(), "E0123") {
		t.Fatalf("want E0123 on a tampered cache, got: %v", err)
	}
}

func TestCacheModuleDirNoCollision(t *testing.T) {
	// Two distinct sources that the old flatten-`/`-to-`_` key would have mapped
	// to one directory (`a/b` ↔ `a_b`) must get distinct cache dirs.
	slash := cacheModuleDir("github.com/x/a/b", "v1.0.0")
	under := cacheModuleDir("github.com/x/a_b", "v1.0.0")
	if slash == under {
		t.Errorf("a/b and a_b sources collide on one cache dir: %s", slash)
	}
}

func runGitVersion() (string, error) { return gitOutput("", "version") }
