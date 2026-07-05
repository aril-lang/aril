package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aril-lang/aril/internal/binding"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

// resolved is the build unit produced by the package resolver: the full,
// ordered set of `.aril` source files (the entry package plus the
// transitive closure of imported user packages) and the set of
// user-package import paths — which are resolved by pulling those files
// into the build, not by a Go import, so codegen strips them from the
// emitted import block.
type resolved struct {
	files       []string        // all .aril files, deterministic order
	userImports map[string]bool // import paths that name a user package
}

// resolvePackages computes the build unit for an entry target. Without a
// manifest (m == nil) the entry is a lone package — its files, no
// cross-package resolution (RFC-0002 §Resolution: step 1 skipped). With
// a manifest, each `import P` is classified (RFC-0002 §Resolution):
//   - a path under the project name → a local user package (its
//     directory's .aril files join the build); a missing directory is
//     E0117.
//   - a path whose root names a declared [dependencies.<name>] → an
//     external module; its .aril source (kind="aril") joins the build and
//     its own imports resolve against its manifest. A declared-but-absent
//     module is E0121.
//   - a stdlib / [bindings] extra path → left for the binding registry.
//   - anything else → E0117 unknown import path.
//
// Cycles in the import graph (across modules) are E0116.
func resolvePackages(entry []string, m *projectManifest) (*resolved, error) {
	r := &resolved{userImports: map[string]bool{}}
	seenDir := map[string]bool{} // package dirs already gathered
	onStack := map[string]bool{} // dirs on the current DFS path (cycle detection)

	// visit carries `mod`, the manifest that governs the imports of the files
	// it is walking — the entry project's manifest, or, once the DFS crosses
	// into an external dependency, that dependency's own manifest (so its
	// imports resolve against its `[project] name` root and its own deps). The
	// acyclic-graph check (D20, E0116) spans module boundaries via the shared
	// abs-dir stack.
	var visit func(dir string, files []string, trail []string, mod *projectManifest) error
	visit = func(dir string, files []string, trail []string, mod *projectManifest) error {
		abs, _ := filepath.Abs(dir)
		if onStack[abs] {
			return fmt.Errorf("aril: error[E0116]: cyclic package import: %s", strings.Join(append(trail, dir), " -> "))
		}
		if seenDir[abs] {
			return nil
		}
		seenDir[abs] = true
		onStack[abs] = true
		defer func() { onStack[abs] = false }()

		for _, f := range files {
			imps, err := fileImports(f)
			if err != nil {
				// Defer the real error to the authoritative parse in
				// compilePackage; skip import discovery for this file.
				continue
			}
			for _, p := range imps {
				kind, target := classifyImport(p, mod)
				switch kind {
				case importStdlib:
					// resolved by the binding registry; nothing to gather.
				case importStd:
					// a compiler-bundled module — injected manifest-
					// independently in buildUnit; nothing to gather here.
				case importUser:
					r.userImports[p] = true
					// A package importing itself (e.g. bare `import
					// myproj` from the project root, or a file re-importing
					// its own package) is a no-op, not a cycle.
					if absTarget, _ := filepath.Abs(target); absTarget == abs {
						continue
					}
					sub, err := gatherSources(target)
					if err != nil {
						return fmt.Errorf("aril: error[E0117]: unknown import path %q (no package directory at %s)", p, target)
					}
					if err := visit(target, sub, append(trail, dir), mod); err != nil {
						return err
					}
				case importExternal:
					// A declared external dependency. Only kind="aril" (pure
					// Aril source, compiled into the build) is wired today;
					// binding/go deps (a Go `require`) are later work.
					d := lookupDep(mod, p)
					if d.kind != "aril" {
						return fmt.Errorf("aril: error[E0121]: dependency %q has kind %q; only kind=\"aril\" external modules are resolved so far", p, d.kind)
					}
					r.userImports[p] = true
					sub, err := gatherSources(target)
					if err != nil {
						return fmt.Errorf("aril: error[E0121]: dependency %q is declared but not present (run `aril get`); no module at %s", p, target)
					}
					subMod, err := findProjectManifest(target)
					if err != nil {
						return err
					}
					if subMod == nil {
						return fmt.Errorf("aril: error[E0121]: external module for import %q has no aril.toml (at or above %s)", p, target)
					}
					if err := visit(target, sub, append(trail, dir), subMod); err != nil {
						return err
					}
				case importUnknown:
					return fmt.Errorf("aril: error[E0117]: unknown import path %q", p)
				}
			}
		}
		r.files = append(r.files, files...)
		return nil
	}

	if err := visit(filepath.Dir(entry[0]), entry, nil, m); err != nil {
		return nil, err
	}
	r.files = dedupeSorted(r.files)
	return r, nil
}

type importKind int

const (
	importStdlib importKind = iota
	importUser
	importUnknown
	importStd      // a compiler-bundled Aril module (`std/pred`), injected in buildUnit
	importExternal // a declared [dependencies.<name>] external module (RFC-0008)
)

// classifyImport decides how an import path resolves (RFC-0002
// §Resolution). For a user package it also returns the directory the
// package lives in (relative to the manifest root).
func classifyImport(p string, m *projectManifest) (importKind, string) {
	head := strings.SplitN(p, "/", 2)[0]
	if isStdModule(p) {
		return importStd, ""
	}
	if m != nil && (p == m.name || strings.HasPrefix(p, m.name+"/")) {
		rest := strings.TrimPrefix(strings.TrimPrefix(p, m.name), "/")
		return importUser, filepath.Join(m.dir, filepath.FromSlash(rest))
	}
	// An import whose root names a declared [dependencies.<name>] resolves into
	// that external module's package tree (RFC-0002 order: after the local
	// project name, before the builtin binding surface). The module root is the
	// `replace` local path or the fetch cache; the package is the remaining path.
	if d := lookupDep(m, p); d != nil {
		rest := strings.TrimPrefix(strings.TrimPrefix(p, d.name), "/")
		return importExternal, filepath.Join(externalModuleRoot(d, m), filepath.FromSlash(rest))
	}
	if binding.IsBuiltinModule(head) {
		return importStdlib, ""
	}
	if m != nil {
		for _, extra := range m.bindingsExtra {
			if lastSegment(extra) == head {
				return importStdlib, ""
			}
		}
	}
	return importUnknown, ""
}

// fileImports lexes + parses one .aril file and returns its import paths.
// Errors are returned so the caller can defer to the authoritative parse.
func fileImports(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks, lerr := lexer.LexFile(string(src), path)
	if lerr != nil {
		return nil, lerr
	}
	tree, perr := parser.ParseFile(toks, path)
	if perr != nil {
		return nil, perr
	}
	var out []string
	for _, im := range tree.Imports {
		out = append(out, im.Path)
	}
	return out, nil
}

// lookupDep returns the declared dependency whose import-path root heads `p`
// (`kv` matches `import kv` and `import kv/store`), or nil. The longest-root
// match wins so a more specific root is preferred, though roots do not nest in
// practice (each is a distinct project name).
func lookupDep(m *projectManifest, p string) *dependency {
	if m == nil {
		return nil
	}
	var best *dependency
	for i := range m.deps {
		d := &m.deps[i]
		if p == d.name || strings.HasPrefix(p, d.name+"/") {
			if best == nil || len(d.name) > len(best.name) {
				best = d
			}
		}
	}
	return best
}

// externalModuleRoot is the on-disk directory of a dependency's module: the
// `replace` local path (relative to the declaring manifest's dir) when given,
// else the fetch cache location. The cache layout is provisional (RFC-0008
// leaves it to the fetch step); today only `replace` deps exist on disk.
func externalModuleRoot(d *dependency, m *projectManifest) string {
	if d.replace != "" {
		r := filepath.FromSlash(d.replace)
		if !filepath.IsAbs(r) && m != nil {
			r = filepath.Join(m.dir, r)
		}
		return r
	}
	return cacheModuleDir(d.source, d.version)
}

// arilCacheDir is the root of the hermetic module cache: $ARIL_CACHE, else the
// per-user cache dir, else a temp fallback (RFC-0008 §fetch & the cache).
func arilCacheDir() string {
	if c := os.Getenv("ARIL_CACHE"); c != "" {
		return c
	}
	if h, err := os.UserCacheDir(); err == nil {
		return filepath.Join(h, "aril")
	}
	return filepath.Join(os.TempDir(), "aril-cache")
}

// cacheModuleDir is where a fetched module version lives in the cache. The
// source path's slashes are flattened so the entry is one directory level.
func cacheModuleDir(source, version string) string {
	safe := strings.ReplaceAll(strings.ReplaceAll(source, "/", "_"), string(filepath.Separator), "_")
	return filepath.Join(arilCacheDir(), safe+"@"+version)
}

func dedupeSorted(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
