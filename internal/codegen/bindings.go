package codegen

import "github.com/aril-lang/aril/internal/binding"

// bindings.go — codegen's view of the stdlib binding surface: thin forwarders
// over the consolidated tables in internal/binding (membership.go) plus the
// codegen-local lowering glue. Data + rationale: architecture.md §binding
// subsystem; surface spelling: binding-surface.md.
//
// Three lowering shapes: rename `pkg.m(a)` → `pkg.GoName(a)` (mapFieldName);
// resultWrap → `ResultOf(pkg.GoName(a))` (emitCall); conversion
// `strings.fromBytes(b)` → `string(b)`, a Go conversion (no import).

// stdlibConversionOf returns the Go conversion target for a conversion
// binding `recv.name`, or ("", false) when the pair is not one.
// (Table lives in binding.conversionTable — shared with sema membership.)
func stdlibConversionOf(recv, name string) (string, bool) {
	return binding.ConversionOf(recv, name)
}

// stdlibCommaOkOf returns the Go identifier for a comma-ok binding
// `recv.name`, or ("", false) when the pair is not one.
// (Table lives in binding.commaOkTable — shared with sema membership.)
func stdlibCommaOkOf(recv, name string) (string, bool) {
	return binding.CommaOkOf(recv, name)
}

// stdlibRenameOf returns the Go identifier for a value/effect-returning
// binding `recv.name`, or ("", false) when recv is not a stdlib
// namespace ident or the pair has no rename entry. Reads the derived
// registry first (D6), then the curation overlay.
func stdlibRenameOf(recv, name string) (string, bool) {
	if !isStdlibNamespaceName(recv) {
		return "", false
	}
	if g, ok := binding.RenameOf(recv, name); ok {
		return g, true
	}
	if g, ok := binding.RenameOverlayOf(recv, name); ok {
		return g, true
	}
	// Value-handle constructor (binding.handleCtors): a package function that
	// builds a stdlib struct handle (regexp.mustCompile → regexp.MustCompile).
	if hc, ok := binding.HandleCtorOf(recv, name); ok {
		return hc.GoName, true
	}
	return "", false
}

// stdlibResultWrapOf returns the Go identifier for a `(T, error)`
// binding `recv.name`, or ("", false) when the pair is not a
// result-wrapping binding. All such bindings are mechanical, so this is
// the derived registry directly (D6).
func stdlibResultWrapOf(recv, name string) (string, bool) {
	return binding.ResultWrapOf(recv, name)
}

// stdlibResultWrapIsUnit reports whether a ResultWrap binding lifts a
// bare `error` (Go referent `func(...) error`, e.g. os.writeFile) — which
// lowers via the `ResultUnit` helper to Result<unit, error> — rather than a
// `(T, error)` pair, which lowers via `ResultOf`. The registry carries the
// distinction in the return spelling (Result<unit, error> vs Result<T, error>).
func stdlibResultWrapIsUnit(recv, name string) bool {
	s, ok := binding.ReturnSpelling(recv, name)
	return ok && s == "Result<unit, error>"
}
