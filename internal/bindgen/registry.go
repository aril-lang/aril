package bindgen

// The stdlib-registry deriver (D6). DeriveRegistry introspects the curated
// binding.Manifest via go/importer source mode and produces the mechanical
// binding facts — Go referent name, lowering kind (rename vs `(T, error)` →
// Result), and the Aril return-type spelling — for each listed symbol.
// RenderRegistry emits the committed internal/binding/registry_gen.go from
// them, so the single derived registry replaces the former hand tables in sema
// and codegen. Only the *mechanical* shapes are accepted; a manifest symbol
// with an idiom-shaped signature (comma-ok, arity ≥ 3, two non-error results)
// is an error — those stay hand-authored wrappers, not registry rows.

import (
	"fmt"
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strings"

	"github.com/aril-lang/aril/internal/binding"
)

// DeriveRegistry introspects binding.Manifest and returns the derived facts,
// sorted by (Pkg, ArilName) for a deterministic registry. An error names the
// first symbol whose signature is not a mechanical binding (or fails to load).
func DeriveRegistry() ([]binding.Fact, error) {
	var facts []binding.Fact
	for _, pkg := range sortedKeys(binding.Manifest) {
		imp := importer.ForCompiler(token.NewFileSet(), "source", nil)
		tp, err := imp.Import(pkg)
		if err != nil {
			return nil, fmt.Errorf("bindgen registry: import %q: %w", pkg, err)
		}
		g := &generator{pkg: tp, path: pkg, qualifyLocal: true}
		for _, sym := range binding.Manifest[pkg] {
			f, err := g.deriveFact(pkg, sym)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", pkg, sym, err)
			}
			facts = append(facts, f)
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Pkg != facts[j].Pkg {
			return facts[i].Pkg < facts[j].Pkg
		}
		return facts[i].ArilName < facts[j].ArilName
	})
	return facts, nil
}

// deriveFact derives one mechanical binding fact for the exported `pkg.goName`.
func (g *generator) deriveFact(pkg, goName string) (binding.Fact, error) {
	obj := g.pkg.Scope().Lookup(goName)
	if obj == nil || !obj.Exported() {
		return binding.Fact{}, fmt.Errorf("not an exported symbol of the package")
	}
	// The registry namespace is the Aril import name — the last path segment of
	// the Go import path, which is also Go's default package selector
	// (`net/http` → `http`, `encoding/json` → `json`). It equals the import path
	// for a single-segment package (`net`, `io`, …), so existing rows are
	// unchanged; a multi-segment package (`net/http`) keys on its base.
	f := binding.Fact{Pkg: path.Base(pkg), ArilName: arilName(goName), GoName: goName}
	switch o := obj.(type) {
	case *types.Func:
		kind, ret, err := g.deriveResults(o.Type().(*types.Signature))
		if err != nil {
			return binding.Fact{}, err
		}
		f.Kind, f.Return = kind, ret
	case *types.Var:
		// A package-level value binding (e.g. os.Args). The call-vs-value
		// distinction is the consumer's (a Field vs a Call node), not the
		// registry's — a value binding is a Rename with a non-call referent.
		ts, ok, reason := g.translate(o.Type())
		if !ok {
			return binding.Fact{}, fmt.Errorf("value type: %s", reason)
		}
		f.Kind, f.Return = binding.Rename, ts
	case *types.Const:
		// A package-level constant (e.g. math.Pi). Like a value binding — a
		// Rename referenced as a value, not called. An untyped constant is
		// translated at its default type (untyped float → float64).
		ts, ok, reason := g.translate(types.Default(o.Type()))
		if !ok {
			return binding.Fact{}, fmt.Errorf("const type: %s", reason)
		}
		f.Kind, f.Return = binding.Rename, ts
	default:
		return binding.Fact{}, fmt.Errorf("unsupported symbol kind %T (not a func or value)", obj)
	}
	return f, nil
}

// deriveResults classifies a function signature's results into the registry's
// mechanical lowering kind + Aril return spelling. Only unit, a single value, a
// bare `error`, and `(T, error)` are mechanical; anything else (comma-ok bool,
// arity ≥ 3, two non-error results) is rejected as an idiom binding.
func (g *generator) deriveResults(sig *types.Signature) (binding.Kind, string, error) {
	res := sig.Results()
	switch res.Len() {
	case 0:
		return binding.Rename, "", nil // unit / effect
	case 1:
		t := res.At(0).Type()
		if isErrorType(t) {
			return binding.ResultWrap, "Result<unit, error>", nil // func(...) error
		}
		ts, ok, reason := g.translate(t)
		if !ok {
			return 0, "", fmt.Errorf("return: %s", reason)
		}
		return binding.Rename, ts, nil
	case 2:
		a, b := res.At(0).Type(), res.At(1).Type()
		if !isErrorType(b) {
			return 0, "", fmt.Errorf("second result is %s, not error — an idiom binding (e.g. comma-ok), not mechanical", b)
		}
		ts, ok, reason := g.translate(a)
		if !ok {
			return 0, "", fmt.Errorf("return: %s", reason)
		}
		return binding.ResultWrap, "Result<" + ts + ", error>", nil
	default:
		return 0, "", fmt.Errorf("%d results — arity ≥ 3 is not a mechanical binding", res.Len())
	}
}

// RenderRegistry derives the registry and renders the committed
// internal/binding/registry_gen.go source (gofmt-clean).
func RenderRegistry() (string, error) {
	facts, err := DeriveRegistry()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("// Code generated by the bindgen deriver; DO NOT EDIT.\n")
	b.WriteString("// Regenerate: go test ./internal/bindgen -run TestRegistryRegen -update\n")
	b.WriteString("// The binding facts are derived from the Go type checker over\n")
	b.WriteString("// binding.Manifest (D6) — never hand-edit a row; edit the Manifest.\n\n")
	b.WriteString("package binding\n\n")
	b.WriteString("var registry = map[[2]string]Fact{\n")
	for _, f := range facts {
		kind := "Rename"
		if f.Kind == binding.ResultWrap {
			kind = "ResultWrap"
		}
		fmt.Fprintf(&b, "\t{%q, %q}: {Pkg: %q, ArilName: %q, GoName: %q, Kind: %s, Return: %q},\n",
			f.Pkg, f.ArilName, f.Pkg, f.ArilName, f.GoName, kind, f.Return)
	}
	b.WriteString("}\n")
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("bindgen registry: gofmt: %w", err)
	}
	return string(out), nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
