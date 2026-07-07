package bindgen

// Module-aware loading (RFC-0010 §Loader). The stdlib deriver imports a package
// by path with the source-mode go/importer, which reaches only GOROOT. To
// introspect a *fetched third-party module* the loader gives the same stdlib
// type-checker the module context it needs, synthesized from the fetched tree:
// a throwaway module whose go.mod requires the target module and replaces it to
// the fetched directory, plus a blank-import anchor so the package is in the
// build graph. Source-mode import then resolves the package through the replace
// and type-checks it — no golang.org/x/tools dependency (D22 preserved).
//
// The chosen loader (option C in the RFC): near-zero code, sufficient for a
// pure-Go module. Its ceiling — cgo-defined API is invisible to source-mode
// type-checking, and go/build module resolution is the legacy path — is the
// documented reason the loader lives behind this one function, swappable for a
// `go list`-driven successor without touching the generator.

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// loaderMu serializes the chdir the source-mode importer needs: go/build resolves
// the synthesized module context relative to the process working directory, so
// the loader chdirs into it for the duration of the import. aril introspects one
// module at a time (a CLI authoring step, not a concurrent build path), and the
// mutex guards against any future concurrent caller corrupting the shared cwd.
var loaderMu sync.Mutex

// LoadModulePackage type-checks the package at importPath from a fetched/local Go
// module rooted at moduleDir (whose go.mod declares module path modulePath), and
// returns its type information. importPath must be modulePath or a package under
// it. version keys the require line (a placeholder for a local replace).
func LoadModulePackage(importPath, moduleDir, modulePath, version string) (*types.Package, error) {
	if modulePath == "" {
		return nil, fmt.Errorf("bindgen: no module path for %q (module %q has no go.mod?)", importPath, moduleDir)
	}
	if importPath != modulePath && !strings.HasPrefix(importPath, modulePath+"/") {
		return nil, fmt.Errorf("bindgen: %q is not a package of module %q", importPath, modulePath)
	}
	absModule, err := filepath.Abs(moduleDir)
	if err != nil {
		return nil, err
	}
	if version == "" {
		version = "v0.0.0"
	}

	ctx, err := os.MkdirTemp("", "aril-bindgen-ctx-*")
	if err != nil {
		return nil, fmt.Errorf("bindgen: synth module context: %w", err)
	}
	defer os.RemoveAll(ctx)

	goMod := fmt.Sprintf("module aril-bindgen-ctx\n\ngo 1.22\n\nrequire %s %s\n\nreplace %s => %s\n",
		modulePath, version, modulePath, absModule)
	if err := os.WriteFile(filepath.Join(ctx, "go.mod"), []byte(goMod), 0o644); err != nil {
		return nil, err
	}
	// A blank-import anchor keeps importPath in the build graph so source-mode
	// import resolves it (a required-but-unimported module is not resolvable).
	anchor := fmt.Sprintf("package anchor\n\nimport _ %q\n", importPath)
	if err := os.WriteFile(filepath.Join(ctx, "anchor.go"), []byte(anchor), 0o644); err != nil {
		return nil, err
	}

	loaderMu.Lock()
	defer loaderMu.Unlock()
	prev, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(ctx); err != nil {
		return nil, fmt.Errorf("bindgen: enter module context: %w", err)
	}
	// Deferred LAST so it runs FIRST (LIFO), before Unlock: cwd is restored to
	// `prev` while the lock is still held, so the next lock-holder's os.Getwd
	// can never capture this call's temp `ctx` as its own restore target. Keep
	// this defer after the Lock's — the ordering is load-bearing.
	defer os.Chdir(prev) //nolint:errcheck // best-effort restore for a one-shot CLI

	imp := importer.ForCompiler(token.NewFileSet(), "source", nil)
	pkg, err := imp.Import(importPath)
	if err != nil {
		return nil, fmt.Errorf("bindgen: loading %q from module %q: %w", importPath, modulePath, err)
	}
	return pkg, nil
}

// GenerateFromModule renders the curated-starting-point Aril binding file for the
// package at importPath in the Go module rooted at moduleDir — the module-aware
// `aril import` (RFC-0010). Unlike Generate (stdlib, GOROOT-only), it introspects
// a fetched/local third-party module.
func GenerateFromModule(importPath, moduleDir, modulePath, version string) (string, error) {
	pkg, err := LoadModulePackage(importPath, moduleDir, modulePath, version)
	if err != nil {
		return "", err
	}
	g := &generator{pkg: pkg, path: importPath}
	return g.run(), nil
}
