package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveOutDirPrecedence checks RFC-0009's resolution order:
// --out-dir flag › ARIL_OUT env › [build] out-dir manifest › ./aril-out.
func TestResolveOutDirPrecedence(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.aril")
	writeFile(t, dir, "main.aril", "func main() {}\n")
	writeFile(t, dir, "aril.toml", "[project]\nname = \"p\"\n\n[build]\nout-dir = \"from-manifest\"\n")

	// Default: no flag, no env, but a manifest key → the manifest dir's from-manifest.
	t.Run("manifest", func(t *testing.T) {
		t.Setenv("ARIL_OUT", "")
		got, err := resolveOutDir(src, "")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, "from-manifest")
		if got != want {
			t.Errorf("manifest out-dir: got %q want %q", got, want)
		}
	})

	// Env beats the manifest — and, being an explicitly-set (shared) out-dir, is
	// namespaced with a per-project segment under from-env.
	t.Run("env-beats-manifest", func(t *testing.T) {
		t.Setenv("ARIL_OUT", filepath.Join(dir, "from-env"))
		got, err := resolveOutDir(src, "")
		if err != nil {
			t.Fatal(err)
		}
		assertNamespacedUnder(t, got, filepath.Join(dir, "from-env"), "p-")
	})

	// The flag beats env and manifest — also namespaced.
	t.Run("flag-wins", func(t *testing.T) {
		t.Setenv("ARIL_OUT", filepath.Join(dir, "from-env"))
		got, err := resolveOutDir(src, filepath.Join(dir, "from-flag"))
		if err != nil {
			t.Fatal(err)
		}
		assertNamespacedUnder(t, got, filepath.Join(dir, "from-flag"), "p-")
	})
}

// assertNamespacedUnder checks that got is base/<segment> where <segment>
// begins with wantPrefix (the project name) — RFC-0009's shared-out-dir layout.
func assertNamespacedUnder(t *testing.T, got, base, wantPrefix string) {
	t.Helper()
	if filepath.Dir(got) != base {
		t.Errorf("out-dir %q not directly under %q", got, base)
	}
	if seg := filepath.Base(got); !strings.HasPrefix(seg, wantPrefix) {
		t.Errorf("project segment %q does not start with %q", seg, wantPrefix)
	}
}

// TestProjectIDDistinctByPath: two projects with the same name at different
// roots get distinct ids (the path hash disambiguates them).
func TestProjectIDDistinctByPath(t *testing.T) {
	m := &projectManifest{name: "app"}
	a := projectID("/home/x/app", m, "/home/x/app/main.aril")
	b := projectID("/home/y/app", m, "/home/y/app/main.aril")
	if a == b {
		t.Errorf("same id for different roots: %q", a)
	}
	if !strings.HasPrefix(a, "app-") {
		t.Errorf("id %q should start with the project name", a)
	}
}

// TestResolveOutDirDefault: with no flag/env/manifest key, the default is
// <project-root>/aril-out — and with no manifest the root is the target's dir.
func TestResolveOutDirDefault(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.aril")
	writeFile(t, dir, "main.aril", "func main() {}\n")
	t.Setenv("ARIL_OUT", "")

	got, err := resolveOutDir(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "aril-out") {
		t.Errorf("default out-dir: got %q want %q", got, filepath.Join(dir, "aril-out"))
	}
}

func TestBinaryBaseName(t *testing.T) {
	cases := map[string]string{
		"a/b/hello.aril": "hello",
		"hello.aril":     "hello",
		"pkgdir":         "pkgdir",
		"a/b/greeter":    "greeter",
	}
	for in, want := range cases {
		if got := binaryBaseName(filepath.FromSlash(in)); got != want {
			t.Errorf("binaryBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWriteProjectModulePersists checks the persisted layout: gen/main.go +
// gen/go.mod + the auto .gitignore, and that a second write reuses the same
// path (persistence — the basis of Go's incremental build cache, RFC-0009).
func TestWriteProjectModulePersists(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "aril-out")
	goSrc := "package main\n\nfunc main() {}\n"

	src, err := writeProjectModule(goSrc, outDir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(outDir, "gen")
	if src.dir != genDir {
		t.Errorf("gen dir: got %q want %q", src.dir, genDir)
	}
	for _, f := range []string{"gen/main.go", "gen/go.mod"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	gi, err := os.ReadFile(filepath.Join(outDir, ".gitignore"))
	if err != nil {
		t.Fatalf("missing .gitignore: %v", err)
	}
	if strings.TrimSpace(string(gi)) != "*" {
		t.Errorf(".gitignore = %q, want \"*\"", gi)
	}

	// A second write persists to the same path (no fresh temp dir).
	src2, err := writeProjectModule(goSrc, outDir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if src2.dir != genDir {
		t.Errorf("second write moved gen: got %q want %q", src2.dir, genDir)
	}
}

// TestGenOrphanSync checks that a persisted gen/ prunes files a later lowering
// no longer emits: a vendored build writes arilrt/, and a following inline build
// (no runtime import) must delete arilrt/ + update the manifest, so `go build`
// never compiles a phantom (RFC-0009 §Persisted).
func TestGenOrphanSync(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "aril-out")
	genDir := filepath.Join(outDir, "gen")

	// Vendored: the program imports the runtime → arilrt/ is emitted.
	vendored := "package main\n\nimport _ \"" + runtimeImportPath + "\"\n\nfunc main() {}\n"
	if _, err := writeProjectModule(vendored, outDir, nil, ""); err != nil {
		t.Fatal(err)
	}
	rt, err := listGoFiles(filepath.Join(genDir, "arilrt"))
	if err != nil || len(rt) == 0 {
		t.Fatalf("vendored build did not emit arilrt/: files=%v err=%v", rt, err)
	}
	man, err := readEmittedManifest(genDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(man) <= 2 { // main.go + go.mod + arilrt files
		t.Errorf("manifest should list arilrt files, got %v", man)
	}

	// Plant a stray non-.go file in arilrt/ (models a future embedded asset that
	// listGoFiles wouldn't enumerate) — the whole dir must still be dropped.
	if err := os.WriteFile(filepath.Join(genDir, "arilrt", "LICENSE"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inline: no runtime import → arilrt/ must be pruned (stray and all).
	inline := "package main\n\nfunc main() {}\n"
	if _, err := writeProjectModule(inline, outDir, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(genDir, "arilrt")); !os.IsNotExist(err) {
		t.Errorf("arilrt/ not pruned after inline build (stat err=%v)", err)
	}
	man2, err := readEmittedManifest(genDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(man2) != 2 {
		t.Errorf("inline manifest should be [main.go go.mod], got %v", man2)
	}
	// The staple files survive.
	for _, f := range []string{"main.go", "go.mod"} {
		if _, err := os.Stat(filepath.Join(genDir, f)); err != nil {
			t.Errorf("%s missing after prune: %v", f, err)
		}
	}
}

// TestBuildDefaultLayoutE2E builds a real example through the aril binary and
// asserts RFC-0009's default layout: the binary lands at <out-dir>/bin/<name>,
// gen/ is persisted, and the .gitignore is written. Built into a temp -out-dir
// so the test never litters the source tree.
func TestBuildDefaultLayoutE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skips the go-toolchain build in -short")
	}
	outDir := t.TempDir()
	ex := filepath.Join(projectRoot(t), "examples", "core-language", "hello", "hello.aril")
	_, stderr, code := runAril(t, "build", "-out-dir", outDir, ex)
	if code != 0 {
		t.Fatalf("aril build failed (exit %d): %s", code, stderr)
	}
	// -out-dir is a shared (explicit) out-dir → the layout is namespaced under a
	// project-id segment; resolveOutDir computes the same final dir the build used.
	final, err := resolveOutDir(ex, outDir)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(final, "bin", "hello")
	if _, err := os.Stat(bin); err != nil {
		t.Errorf("binary not at %s: %v", bin, err)
	}
	if _, err := os.Stat(filepath.Join(final, "gen", "main.go")); err != nil {
		t.Errorf("gen/main.go not persisted: %v", err)
	}
	// The .gitignore is written at the project-local root (the segment here); its
	// "*" self-ignores the segment, keeping the shared out-dir git-clean.
	if _, err := os.Stat(filepath.Join(final, ".gitignore")); err != nil {
		t.Errorf(".gitignore not written: %v", err)
	}
}

// TestBuildLockSequential checks the advisory lock is acquired, released, and
// re-acquirable (a sequential build after a prior one completes).
func TestBuildLockSequential(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireBuildLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	release()
	release2, err := acquireBuildLock(dir)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	release2()
}
