package codegen

import "github.com/aril-lang/aril/internal/binding"

// bindings.go — the codegen side of the stdlib binding surface. The
// *mechanical* rows (a value/effect rename, or a `(T, error)` → Result
// lift) are no longer hand-written here: they are derived from the Go
// type checker and read from the `internal/binding` registry (D6), the
// single source `internal/sema` shares. This file keeps only the
// *idiom* rows that are not mechanical signature transforms — the
// `fmt.print*` effect renames and `strings.fromBytes` conversion (the
// time-duration constructors and the runtime-helper bindings live in
// emitCall). Source of truth for the intended surface is
// `docs/binding-surface.md`.
//
// Three lowering shapes:
//   - rename:     `pkg.method(args)` → `pkg.GoName(args)` (or a value
//                 reference `pkg.GoName`). Registry Rename rows +
//                 stdlibRenameOverlay. Handled by mapFieldName.
//   - resultWrap: `pkg.method(args)` → `ResultOf(pkg.GoName(args))`.
//                 Registry ResultWrap rows. Handled by emitCall.
//   - conversion: `strings.fromBytes(b)` → `string(b)` — a Go type
//                 conversion, not a package call (needs no import).

// stdlibRenameOverlay holds the value/effect renames the derived
// registry deliberately excludes because they are a curation choice,
// not a mechanical signature transform: `fmt.Print/Printf/Println`
// return `(int, error)` in Go, but the Aril surface treats them as
// fire-and-forget effects that discard the count+error (binding.Manifest
// §curation note). They lower like any other rename.
var stdlibRenameOverlay = map[[2]string]string{
	{"fmt", "println"}: "Println",
	{"fmt", "print"}:   "Print",
	{"fmt", "printf"}:  "Printf",
	// Bare-`error` *constructors* (binding.Manifest §curation note): the
	// returned `error` IS the value, not a failure signal, so they are a
	// bare-`error` Rename here — NOT a registry ResultWrap row (which would
	// wrongly lift them to Result<unit, error>). errors.new mirrors the
	// `error(msg)` built-in; fmt.errorf adds `%w` wrapping.
	{"errors", "new"}: "New",
	{"fmt", "errorf"}: "Errorf",
}

// timeDurationUnit maps a `time.<ctor>(n)` Duration constructor to
// its Go `time.<Unit>` constant. The call lowers to
// `time.Duration(n) * time.<Unit>` (binding-surface.md §time —
// Aril hides Go's `time.Second * N` idiom behind factory funcs).
// ("", false) when name is not a Duration constructor.
func timeDurationUnit(name string) (string, bool) {
	switch name {
	case "seconds":
		return "Second", true
	case "milliseconds":
		return "Millisecond", true
	}
	return "", false
}

// stdlibConversion maps a binding that lowers to a Go *type
// conversion* `<target>(arg)` rather than a package call — so it pulls
// in no import (binding-surface.md). Single source of truth for both
// the lowering (emitCall) and the import-suppression check
// (isConversionBinding); a divergence between the two would mis-track
// imports.
var stdlibConversion = map[[2]string]string{
	{"strings", "fromBytes"}: "string", // []byte → string
	{"strings", "toBytes"}:   "[]byte", // string → []byte
}

// stdlibConversionOf returns the Go conversion target for a conversion
// binding `recv.name`, or ("", false) when the pair is not one.
func stdlibConversionOf(recv, name string) (string, bool) {
	g, ok := stdlibConversion[[2]string{recv, name}]
	return g, ok
}

// stdlibCommaOk maps a binding whose Go referent returns a comma-ok
// `(T, bool)` pair to its Go identifier; the call lowers to
// `OptionOf(pkg.GoName(args))` (binding-surface.md §os — lookupEnv
// distinguishes an unset variable from an empty one). These are idiom
// bindings: the deriver rejects a `(T, bool)` result as non-mechanical.
var stdlibCommaOk = map[[2]string]string{
	{"os", "lookupEnv"}: "LookupEnv", // (string, bool) → Option<string>
}

// stdlibCommaOkOf returns the Go identifier for a comma-ok binding
// `recv.name`, or ("", false) when the pair is not one.
func stdlibCommaOkOf(recv, name string) (string, bool) {
	g, ok := stdlibCommaOk[[2]string{recv, name}]
	return g, ok
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
	g, ok := stdlibRenameOverlay[[2]string{recv, name}]
	return g, ok
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
