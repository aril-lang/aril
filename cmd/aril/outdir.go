package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	aril "github.com/aril-lang/aril"
)

// Build-artifact layout (RFC-0009). A build writes into a single per-project
// directory, `aril-out/` by default:
//
//	aril-out/
//	  bin/<name>     the final native binary (default `aril build` target)
//	  gen/           the lowered Go module (the IR), persisted across builds
//	  .gitignore     "*" — auto-generated, so artifacts stay untracked
//
// Persisting `gen/` (rather than lowering to a throwaway temp dir) holds the
// source path stable so Go's $GOCACHE makes an unchanged rebuild incremental
// (RFC-0009 §Persisted). Go stays an IR the developer never works in (D1/D16);
// its on-disk presence is incidental. The REPL keeps its own throwaway temp
// (writeTempModule) — it is projectless and has no aril-out/ to persist into.

// resolveOutDir returns the absolute build-output directory for a target,
// per RFC-0009's precedence: --out-dir flag › ARIL_OUT env › [build] out-dir
// manifest key › the default ./aril-out. Flag and env are resolved relative to
// the cwd (mirroring Cargo's --target-dir / CARGO_TARGET_DIR); the manifest key
// and the default are relative to the project root (the manifest dir, or the
// target's own directory when there is no manifest).
func resolveOutDir(srcPath, flagOutDir string) (string, error) {
	root, manifest, err := projectOutputRoot(srcPath)
	if err != nil {
		return "", err
	}
	envOut := os.Getenv("ARIL_OUT")
	var chosen string
	switch {
	case flagOutDir != "":
		chosen = flagOutDir
	case envOut != "":
		chosen = envOut
	case manifest != nil && manifest.buildOutDir != "":
		chosen = filepath.Join(root, manifest.buildOutDir)
	default:
		chosen = filepath.Join(root, "aril-out")
	}
	abs, err := filepath.Abs(chosen)
	if err != nil {
		return "", fmt.Errorf("aril: resolve out-dir: %w", err)
	}
	return abs, nil
}

// projectOutputRoot returns the project root for a build target and its
// manifest (nil if none). The root is the manifest's directory when an
// aril.toml governs the target, else the target's own directory — a lone
// stdlib-only file needs no manifest (RFC-0002 §Resolution).
func projectOutputRoot(srcPath string) (string, *projectManifest, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", nil, fmt.Errorf("aril: cannot stat %s: %w", srcPath, err)
	}
	srcDir := srcPath
	if !info.IsDir() {
		srcDir = filepath.Dir(srcPath)
	}
	manifest, err := findProjectManifest(srcDir)
	if err != nil {
		return "", nil, err
	}
	root := srcDir
	if manifest != nil {
		root = manifest.dir
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("aril: resolve project root: %w", err)
	}
	return absRoot, manifest, nil
}

// binaryBaseName derives the default binary name from a build target:
// "…/hello.aril" → "hello", a package directory "…/greeter" → "greeter".
func binaryBaseName(srcPath string) string {
	base := filepath.Base(srcPath)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return base
}

// writeProjectModule writes the lowered Go program into the persisted
// <outDir>/gen module (RFC-0009): main.go + go.mod, plus the vendored arilrt
// subpackage when the program imports it, and the auto `.gitignore` at the
// out-dir root. Unlike writeTempModule it does not create a throwaway temp dir
// and the caller must NOT remove it — persistence is the whole point (Go's
// build cache keys on the stable path). Returns the gen dir as the build cwd.
func writeProjectModule(goSrc, outDir string) (*compiledSource, error) {
	goMod, err := thirdPartyGoMod(goSrc)
	if err != nil {
		return nil, err
	}
	if err := writeOutDirGitignore(outDir); err != nil {
		return nil, err
	}
	genDir := filepath.Join(outDir, "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return nil, fmt.Errorf("aril: mkdir gen: %w", err)
	}
	// emitted collects the gen-relative paths this lowering produced, so the
	// next lowering can prune whatever it no longer emits (RFC-0009 §Persisted).
	emitted := []string{"main.go", "go.mod"}
	if err := os.WriteFile(filepath.Join(genDir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return nil, fmt.Errorf("aril: write main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return nil, fmt.Errorf("aril: write go.mod: %w", err)
	}
	// Vendored-mode programs import the arilrt runtime as a subpackage; copy the
	// embedded sources into <gen>/arilrt so `go build/run .` resolves it (D18
	// CT2). Keyed on the actual import so the two stay in step.
	if strings.Contains(goSrc, `"`+runtimeImportPath+`"`) {
		if _, err := aril.WriteVendoredRuntime(genDir); err != nil {
			return nil, fmt.Errorf("aril: vendor runtime: %w", err)
		}
		rt, err := listGoFiles(filepath.Join(genDir, "arilrt"))
		if err != nil {
			return nil, err
		}
		for _, f := range rt {
			emitted = append(emitted, filepath.Join("arilrt", f))
		}
	}
	// Prune orphans: a persisted gen/ must delete what a prior lowering wrote but
	// this one no longer emits (a vendored→inline switch drops arilrt/) — keeping
	// gen/ in sync with the sources and forward-proofing multi-file emission
	// against a stale `.go` compiling as a phantom (RFC-0009 §Persisted).
	if err := syncEmitted(genDir, emitted); err != nil {
		return nil, err
	}
	return &compiledSource{dir: genDir}, nil
}

// emittedManifest is the dotfile under gen/ recording the set of files the last
// lowering emitted. A leading dot keeps the Go tool from compiling it as source
// (cmd/go ignores files beginning with "." or "_").
const emittedManifest = ".aril-emitted"

// listGoFiles returns the *.go file names (not paths) directly in dir, sorted.
func listGoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("aril: read %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// syncEmitted removes every gen-relative path the previous lowering recorded
// but the current one (emitted) did not, empties the arilrt/ dir when it goes
// unused, then rewrites the manifest. Orphan removal is best-effort: a file
// already gone is not an error.
func syncEmitted(genDir string, emitted []string) error {
	current := make(map[string]bool, len(emitted))
	for _, p := range emitted {
		current[p] = true
	}
	prev, err := readEmittedManifest(genDir)
	if err != nil {
		return err
	}
	for _, p := range prev {
		if current[p] {
			continue
		}
		if err := os.Remove(filepath.Join(genDir, p)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("aril: prune orphan %s: %w", p, err)
		}
	}
	// Drop any subdirectory the previous emit used but the current one does not
	// (arilrt/ after a vendored→inline switch). RemoveAll, not a per-file Remove,
	// so a stray non-tracked file can't strand a half-pruned dir — the enumerated
	// set (listGoFiles) is `.go`-only, but the dir must go whole. A dir still
	// referenced by the current emit is kept.
	curDirs := dirSet(emitted)
	for d := range dirSet(prev) {
		if curDirs[d] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(genDir, d)); err != nil {
			return fmt.Errorf("aril: prune orphan dir %s: %w", d, err)
		}
	}
	return writeEmittedManifest(genDir, emitted)
}

// dirSet returns the set of parent directories (gen-relative, excluding ".")
// referenced by a set of emitted paths.
func dirSet(paths []string) map[string]bool {
	dirs := map[string]bool{}
	for _, p := range paths {
		if d := filepath.Dir(p); d != "." {
			dirs[d] = true
		}
	}
	return dirs
}

// readEmittedManifest returns the gen-relative paths recorded by the last
// lowering (nil when the manifest is absent — a fresh gen/).
func readEmittedManifest(genDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(genDir, emittedManifest))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("aril: read emitted manifest: %w", err)
	}
	var paths []string
	for _, ln := range strings.Split(string(data), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			paths = append(paths, filepath.FromSlash(ln))
		}
	}
	return paths, nil
}

// writeEmittedManifest records the emitted set (slash-normalized, sorted) so the
// manifest is stable across platforms.
func writeEmittedManifest(genDir string, emitted []string) error {
	slashed := make([]string, len(emitted))
	for i, p := range emitted {
		slashed[i] = filepath.ToSlash(p)
	}
	sort.Strings(slashed)
	body := strings.Join(slashed, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(genDir, emittedManifest), []byte(body), 0o644); err != nil {
		return fmt.Errorf("aril: write emitted manifest: %w", err)
	}
	return nil
}

// writeOutDirGitignore writes <outDir>/.gitignore = "*" so every artifact stays
// untracked even if the developer never adds /aril-out to the project .gitignore
// (the discipline Cargo applies to target/, RFC-0009 §aril-out). Idempotent.
func writeOutDirGitignore(outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("aril: mkdir out-dir: %w", err)
	}
	path := filepath.Join(outDir, ".gitignore")
	if err := os.WriteFile(path, []byte("*\n"), 0o644); err != nil {
		return fmt.Errorf("aril: write .gitignore: %w", err)
	}
	return nil
}
