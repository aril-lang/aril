package main

import (
	"crypto/sha256"
	"encoding/hex"
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
	goDeps      []thirdPartyDep // require+replace entries for kind="go"/"binding" Go-binding deps (RFC-0010)
}

// bindingOwner identifies the binding table that binds a Go module (E0124
// uniqueness): the dep name for the message, and the table's absolute path as
// the identity — so the same table reached from two files dedups, while two
// distinct tables binding one Go module conflict even if identically named.
type bindingOwner struct {
	name  string
	table string
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
func resolvePackages(entry []string, m *projectManifest, resolvedVers map[string]string) (*resolved, error) {
	r := &resolved{userImports: map[string]bool{}}
	seenDir := map[string]bool{}         // package dirs already gathered
	onStack := map[string]bool{}         // dirs on the current DFS path (cycle detection)
	boundBy := map[string]bindingOwner{} // Go module path → the table that binds it (E0124 uniqueness)

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
				kind, target := classifyImport(p, mod, resolvedVers)
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
					// A declared external dependency.
					d := lookupDep(mod, p)
					// kind="go" (RFC-0010): a raw Go module bound through a
					// consumer-owned table. The table (extern decls) is a file in
					// *this* project; compile it into the build and strip the
					// import. The bound Go module rides require+replace
					// (thirdparty.go), replace-target = the fetched cache (or a
					// local `replace`). It is a leaf — no aril.toml to expand.
					if d.kind == "go" {
						if d.path == "" {
							return fmt.Errorf("aril: error[E0121]: dependency %q (kind=\"go\") declares no binding table (`path`)", p)
						}
						table := filepath.Join(mod.dir, filepath.FromSlash(d.path))
						tableFiles, err := gatherSources(table)
						if err != nil {
							return fmt.Errorf("aril: error[E0121]: dependency %q: cannot read its binding table %q: %v", p, d.path, err)
						}
						r.userImports[p] = true
						r.files = append(r.files, tableFiles...)
						replaceDir, _ := filepath.Abs(externalModuleRoot(d, mod, resolvedVers))
						// A non-replace kind="go" dep must be fetched first — its
						// cache dir carries the module's go.mod (the authoritative
						// module path). An absent dir means it was never fetched.
						if _, err := os.Stat(replaceDir); err != nil {
							return fmt.Errorf("aril: error[E0121]: dependency %q (kind=\"go\") is not present (run `aril get`, or point `replace` at a local copy)", p)
						}
						// The Go module path — the require/replace key and the @go
						// prefix the table binds — is the module's *own* go.mod
						// `module` directive (authoritative: a repo's git URL may
						// differ from its module path — vanity imports, gopkg.in).
						// Fall back to the declared `source` only if the go.mod is
						// unreadable.
						modulePath := readGoModuleName(replaceDir)
						if modulePath == "" {
							modulePath = d.source
						}
						if modulePath == "" {
							return fmt.Errorf("aril: error[E0121]: dependency %q (kind=\"go\"): cannot determine the bound Go module path — set `source`, or ensure %q has a go.mod", p, replaceDir)
						}
						// At most one binding table may bind a given Go module
						// across the build graph — two tables would emit duplicate
						// `extern` declarations of the same Go symbols into the one
						// lowered module (the Cargo `links` invariant). A repeated
						// import of the *same* dep is a no-op; a *different* dep
						// binding the same module is E0124.
						if prev, ok := boundBy[modulePath]; ok {
							if prev.table != table {
								return fmt.Errorf("aril: error[E0124]: Go module %q is bound by two dependencies (%q and %q); at most one binding table may bind a Go module", modulePath, prev.name, d.name)
							}
							continue // the same table reached again (e.g. imported from two files)
						}
						boundBy[modulePath] = bindingOwner{name: d.name, table: table}
						r.goDeps = append(r.goDeps, thirdPartyDep{
							ImportPath: modulePath,
							Module:     modulePath,
							Version:    depConcreteVersion(d, resolvedVers),
							Vendor:     replaceDir,
						})
						continue
					}
					// The module manifest is anchored at the module root, not
					// found by walking up from the package dir (which could
					// bind an ancestor's aril.toml). A module absent or without
					// its own aril.toml is "not fetched" → E0121; a *present*
					// module missing this sub-package is an unknown path within
					// it → E0117 (running `aril get` cannot fix a typo).
					moduleRoot := externalModuleRoot(d, mod, resolvedVers)
					subMod, err := manifestAt(moduleRoot)
					if err != nil {
						return err
					}
					if subMod == nil {
						return fmt.Errorf("aril: error[E0121]: dependency %q is not present (run `aril get`); no module (aril.toml) at %s", p, moduleRoot)
					}
					// The governing kind is the dependency's self-declared
					// [package].kind; a consumer's [dep].kind, if present, is a
					// guard that must agree (RFC-0008 §`[dep.<name>]`).
					if err := depKindGuard(d.name, d.kind, subMod.packageKind); err != nil {
						return err
					}
					k := effectiveDepKind(d.kind, subMod.packageKind)
					if k != "aril" && k != "binding" {
						return fmt.Errorf("aril: error[E0121]: dependency %q has kind %q; not a resolvable external module", p, k)
					}
					r.userImports[p] = true
					sub, err := gatherSources(target)
					if err != nil {
						return fmt.Errorf("aril: error[E0117]: unknown import path %q (no package in the %q module at %s)", p, d.name, target)
					}
					if err := visit(target, sub, append(trail, dir), subMod); err != nil {
						return err
					}
					// kind="binding" (RFC-0010): a published binding package —
					// kind="aril"-style source composition (its `extern` .aril source
					// joined the build above) *plus* a Go `require`+`replace` for the
					// bound Go module it self-declares (`[package] binds`/`binds-go`),
					// so the extern `@go` targets resolve. Registered once per bound
					// module (E0124 uniqueness spans kind=go and kind=binding).
					if k == "binding" && subMod.binds != "" {
						// The bound module is fetched by its `binds` git source but
						// cached by that same coordinate; its require/replace *key* is
						// the module's own go.mod `module` directive (authoritative —
						// a vanity path like golang.org/x/… differs from its repo URL,
						// and E0124 must key on the same path kind=go uses).
						boundDir, _ := filepath.Abs(cacheModuleDir(subMod.binds, subMod.bindsGo))
						if _, err := os.Stat(boundDir); err != nil {
							return fmt.Errorf("aril: error[E0121]: dependency %q binds Go module %q@%q, not present (run `aril get`)", p, subMod.binds, subMod.bindsGo)
						}
						modulePath := readGoModuleName(boundDir)
						if modulePath == "" {
							modulePath = subMod.binds
						}
						// The binding package's own aril.toml is its binding identity
						// (a kind=binding dep has no consumer `path` table).
						ident := filepath.Join(subMod.dir, "aril.toml")
						if prev, ok := boundBy[modulePath]; ok {
							if prev.table != ident {
								return fmt.Errorf("aril: error[E0124]: Go module %q is bound by two dependencies (%q and %q); at most one binding table may bind a Go module", modulePath, prev.name, d.name)
							}
						} else {
							boundBy[modulePath] = bindingOwner{name: d.name, table: ident}
							r.goDeps = append(r.goDeps, thirdPartyDep{
								ImportPath: modulePath,
								Module:     modulePath,
								Version:    subMod.bindsGo,
								Vendor:     boundDir,
							})
						}
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
func classifyImport(p string, m *projectManifest, resolvedVers map[string]string) (importKind, string) {
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
		return importExternal, filepath.Join(externalModuleRoot(d, m, resolvedVers), filepath.FromSlash(rest))
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
// else the fetch cache location keyed by the *resolved concrete version*. An
// exact pin is self-describing; a ranged constraint's resolved version comes
// from the root lock (resolvedVers, source → version), which `aril get` wrote.
// An unlocked ranged dep resolves to an absent cache dir → E0121 (run `aril
// get`) downstream.
func externalModuleRoot(d *dependency, m *projectManifest, resolvedVers map[string]string) string {
	if d.replace != "" {
		r := filepath.FromSlash(d.replace)
		if !filepath.IsAbs(r) && m != nil {
			r = filepath.Join(m.dir, r)
		}
		return r
	}
	return cacheModuleDir(d.source, depConcreteVersion(d, resolvedVers))
}

// depConcreteVersion is the concrete version keying a dependency's cache dir:
// an exact pin (tag or SHA) verbatim, else the version the root lock recorded
// for its `source` ("" when unlocked).
func depConcreteVersion(d *dependency, resolvedVers map[string]string) string {
	if cons, err := parseConstraint(d.version); err == nil {
		if pin, ok := cons.exactPin(); ok {
			return pin
		}
	}
	return resolvedVers[d.source]
}

// manifestAt loads the aril.toml directly in dir (not by walking up, unlike
// findProjectManifest) — the anchor for an external module, whose manifest must
// live at its own root. Returns (nil, nil) when dir has no aril.toml.
func manifestAt(dir string) (*projectManifest, error) {
	p := filepath.Join(dir, "aril.toml")
	if _, err := os.Stat(p); err != nil {
		return nil, nil
	}
	return parseProjectManifest(p)
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

// cacheModuleDir is where a fetched module version lives in the cache
// (RFC-0008 §fetch & the cache — coordinate-addressed by source + resolved
// version). The key is a readable last-segment prefix plus a hash of the *full*
// source, so distinct sources that would flatten to the same name — the
// `a/b` ↔ `a_b` collision — get distinct directories. The version is a resolved
// concrete tag or commit SHA (RFC-0008: an exact pin normalises via depConcrete-
// Version before reaching here), so it is already a filesystem-safe token.
func cacheModuleDir(source, version string) string {
	h := sha256.Sum256([]byte(source))
	key := lastSegment(source) + "-" + hex.EncodeToString(h[:])[:12]
	return filepath.Join(arilCacheDir(), key+"@"+version)
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
