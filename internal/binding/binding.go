// Package binding is the derived stdlib binding registry (D6). It holds the
// *mechanical* binding facts the corpus's builtin-module surface uses — the Go
// referent name, the lowering shape, and the Aril return-type spelling — for
// each `pkg.method` the corpus calls. The facts are **derived** from the Go
// type checker (go/importer source mode) over the curated Manifest, not
// hand-written: `internal/bindgen` introspects the Manifest and renders
// registry_gen.go, which is committed and is the single source both
// `internal/sema` and `internal/codegen` read — collapsing what was three
// drift-prone hand tables (codegen rename/resultWrap, sema return types) into
// one derived registry.
//
// This package is **pure data** (no go/importer dependency), so the compiler
// build reads the committed registry without a Go source tree; only
// *regeneration* (the bindgen deriver + drift-guard test) needs GOROOT src.
//
// Scope: the *mechanical* slice only — a value/effect rename or a `(T, error)`
// → Result lift, both pure signature transforms. The *idiom* bindings
// (`fmt.scan*`, `json.parse/serialize`, `sort.sorted`, the time-duration
// constructors, `strings.fromBytes`) are not mechanical signature transforms
// and stay hand-authored in the consumers (the architecture.md §3 "idiomatic
// wrapper" layer).
package binding

// Kind is the lowering shape of a derived binding.
type Kind uint8

const (
	// Rename — a value- or effect-returning referent: `pkg.method(args)`
	// lowers to `pkg.GoName(args)` (or a value reference `pkg.GoName` for a
	// non-call binding) with only an identifier swap.
	Rename Kind = iota
	// ResultWrap — a `(T, error)`-returning referent: the call lowers to
	// `ResultOf(pkg.GoName(args))`, folding the two-value Go return into the
	// predeclared `Result<T, error>`.
	ResultWrap
)

// Fact is one derived stdlib binding: the Aril `Pkg.ArilName` surface, its Go
// referent `GoName`, the lowering Kind, and the Aril return-type spelling sema
// types the call as (`Return` — "" for a unit/effect return; for a ResultWrap
// it is the full `Result<T, error>` spelling).
type Fact struct {
	Pkg      string
	ArilName string
	GoName   string
	Kind     Kind
	Return   string
}

// Lookup returns the derived Fact for `pkg.arilName`, or ok=false when the pair
// is not a registered mechanical binding.
func Lookup(pkg, arilName string) (Fact, bool) {
	f, ok := registry[[2]string{pkg, arilName}]
	return f, ok
}

// RenameOf returns the Go identifier for a value/effect-returning binding
// `pkg.arilName` (Kind == Rename), or ("", false) otherwise.
func RenameOf(pkg, arilName string) (string, bool) {
	if f, ok := registry[[2]string{pkg, arilName}]; ok && f.Kind == Rename {
		return f.GoName, true
	}
	return "", false
}

// ResultWrapOf returns the Go identifier for a `(T, error)` binding
// `pkg.arilName` (Kind == ResultWrap), or ("", false) otherwise.
func ResultWrapOf(pkg, arilName string) (string, bool) {
	if f, ok := registry[[2]string{pkg, arilName}]; ok && f.Kind == ResultWrap {
		return f.GoName, true
	}
	return "", false
}

// ReturnSpelling returns the Aril return-type spelling of `pkg.arilName`, or
// ("", false) when the pair is not registered. The spelling is "" for a
// unit/effect return; sema maps a non-empty spelling to its own Type.
func ReturnSpelling(pkg, arilName string) (string, bool) {
	if f, ok := registry[[2]string{pkg, arilName}]; ok {
		return f.Return, true
	}
	return "", false
}
