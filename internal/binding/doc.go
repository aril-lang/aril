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
