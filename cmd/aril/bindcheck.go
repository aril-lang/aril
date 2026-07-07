package main

// bindcheck.go — E0126, the kind="go" binding-table drift check (RFC-0010). A
// consumer-owned table's `extern … @go("mod/pkg.Sym")` declarations are
// assertions about a *pinned* Go module's surface; if a named symbol is absent
// (renamed or removed upstream), the table is stale. Validated at `aril get`
// time — when the module is freshly fetched — so drift is a loud Aril-coordinate
// diagnostic pointing at the offending `extern`, not a silent shrink or a raw
// `go build` miss (the improvement over prior-art binding tables, which drift
// silently). Runs on the serial `aril get` path, so the loader's chdir is safe.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/bindgen"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

// validateGoBindingTables checks every kind="go" dependency's table against its
// freshly-fetched module (E0126). A module whose types cannot be loaded in this
// environment (no GOROOT source tree, a cgo-only surface) is skipped, not failed
// — sound over red, mirroring the bindgen test convention.
func validateGoBindingTables(m *projectManifest, resolvedVers map[string]string) error {
	for i := range m.deps {
		d := &m.deps[i]
		if d.kind != "go" || d.path == "" {
			continue
		}
		moduleDir, _ := filepath.Abs(externalModuleRoot(d, m, resolvedVers))
		modulePath := readGoModuleName(moduleDir)
		if modulePath == "" {
			modulePath = d.source
		}
		if modulePath == "" {
			continue // E0121 territory (handled at build); nothing to validate against
		}
		refs, err := tableGoRefs(filepath.Join(m.dir, filepath.FromSlash(d.path)))
		if err != nil {
			return err
		}
		if err := validateRefs(d.name, refs, moduleDir, modulePath, depConcreteVersion(d, resolvedVers)); err != nil {
			return err
		}
	}
	return nil
}

// goRef is one `@go` referent a table declares: the Go import path, the symbol,
// and the Aril name (for the message).
type goRef struct {
	importPath string
	symbol     string
	arilName   string
}

// tableGoRefs parses a binding table and returns the top-level extern func/type
// `@go` referents (the symbols the module must export). Impl-method refs are
// bare names checked against a handle's method set — out of this name-level pass.
func tableGoRefs(tablePath string) ([]goRef, error) {
	files, err := gatherSources(tablePath)
	if err != nil {
		return nil, fmt.Errorf("aril: error[E0121]: cannot read binding table %q: %v", tablePath, err)
	}
	var refs []goRef
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		toks, lerr := lexer.LexFile(string(src), f)
		if lerr != nil {
			continue // the authoritative parse at build reports lex/parse errors
		}
		tree, perr := parser.ParseFile(toks, f)
		if perr != nil {
			continue
		}
		for _, decl := range tree.Decls {
			switch e := decl.(type) {
			case *ast.ExternFuncDecl:
				if ip, sym, ok := splitGoRef(e.Go, e.Name); ok {
					refs = append(refs, goRef{ip, sym, e.Name})
				}
			case *ast.ExternTypeDecl:
				if ip, sym, ok := splitGoRef(e.Go, e.Name); ok {
					refs = append(refs, goRef{ip, sym, e.Name})
				}
			}
		}
	}
	return refs, nil
}

// splitGoRef splits an `@go("import/path.Sym")` referent into (importPath, symbol).
// ok is false when the attribute is absent (nothing to validate). Mirrors the
// codegen split: the last `.` after the last `/` separates the symbol.
func splitGoRef(ref *ast.GoRef, arilName string) (importPath, symbol string, ok bool) {
	if ref == nil || ref.Raw == "" {
		return "", "", false
	}
	raw := ref.Raw
	slash := strings.LastIndex(raw, "/")
	if dot := strings.LastIndex(raw, "."); dot > slash {
		return raw[:dot], raw[dot+1:], true
	}
	// A bare import path with no `.Symbol` (an extern type spelled `@go("pkg")`):
	// the symbol defaults to the exported Aril name.
	return raw, exportedGoName(arilName), true
}

// exportedGoName upper-cases the first rune (the default Go referent for a bare
// `@go("pkg")` — a lower-cased Aril name maps back to its exported Go spelling).
func exportedGoName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// validateRefs loads each referenced package of the module and checks every
// named symbol is exported by it; an absent symbol is E0126.
func validateRefs(depName string, refs []goRef, moduleDir, modulePath, version string) error {
	byPkg := map[string][]goRef{}
	for _, r := range refs {
		byPkg[r.importPath] = append(byPkg[r.importPath], r)
	}
	for importPath, prs := range byPkg {
		pkg, err := bindgen.LoadModulePackage(importPath, moduleDir, modulePath, version)
		if err != nil {
			// Unloadable in this environment (no GOROOT src / cgo surface): skip,
			// don't fail get — a build against a genuinely-missing symbol still
			// surfaces at go build; E0126 is the early, precise signal when loadable.
			continue
		}
		scope := pkg.Scope()
		for _, r := range prs {
			obj := scope.Lookup(r.symbol)
			if obj == nil || !obj.Exported() {
				return fmt.Errorf("aril: error[E0126]: dependency %q binding table: `%s` binds %s.%s, but the module exports no such symbol (renamed or removed upstream — re-run `aril import` and update the table)",
					depName, r.arilName, importPath, r.symbol)
			}
		}
	}
	return nil
}
