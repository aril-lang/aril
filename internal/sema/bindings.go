package sema

import (
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/binding"
)

// bindings.go — the sema side of the stdlib binding surface. The *mechanical*
// return types (a value/effect rename, or a `(T, error)` → Result lift) are
// derived from the Go type checker and read from the `internal/binding`
// registry (D6) — the single source codegen shares — so they can no longer
// drift from codegen's lowering (the failure #43 hit). This file keeps only the
// *idiom* rows the registry deliberately excludes (the json serialize family,
// which lowers via a runtime helper, not a mechanical binding).
//
// The registry carries the return as an Aril type *spelling*
// (binding.Fact.Return); semaTypeFromSpelling maps it to a sema Type.

// stdlibBindingReturn returns the modelled Aril return type of a stdlib
// binding `pkg.method(...)`, or nil when the pair is not tabled or is a
// unit/effect binding. Knowing the return type lets a `match`/`try` subject
// over a binding call type instead of staying Unknown.
func (c *checker) stdlibBindingReturn(pkg, method string) Type {
	// Idiom rows the derived registry does not carry: json.serialize(v) /
	// serializeIndent(v, prefix, indent) → Result<[]byte, error>
	// (binding-surface.md §encoding/json), lowered via a runtime helper.
	switch [2]string{pkg, method} {
	case [2]string{"json", "serialize"}, [2]string{"json", "serializeIndent"}:
		return &Result{T: &Slice{Elem: &Builtin{N: "byte"}}, E: &Builtin{N: "error"}}
	// Bare-`error` constructors (codegen stdlibRenameOverlay): the returned
	// `error` is the value, not a Result failure-signal (binding-surface.md
	// §errors / §fmt). errors.new mirrors the `error(msg)` built-in.
	case [2]string{"errors", "new"}, [2]string{"fmt", "errorf"}:
		return &Builtin{N: "error"}
	// Comma-ok `(T, bool)` → Option<T> binding (codegen stdlibCommaOk):
	// os.lookupEnv distinguishes an unset env var from an empty one
	// (binding-surface.md §os).
	case [2]string{"os", "lookupEnv"}:
		return &Option{T: &Builtin{N: "string"}}
	// Duration constructors (codegen timeDurationUnit): type the result as the
	// time.Duration handle so its arithmetic method set (add/mul/string)
	// resolves (VALUE-HANDLES / binding-surface.md §time).
	case [2]string{"time", "seconds"}, [2]string{"time", "milliseconds"}:
		return &Named{N: "time.Duration"}
	// Fixed-return `slices` helpers (codegen stdlibRenameOverlay); the
	// element-typed ones (slices.max/min/reverse) stay generic-inferred by Go.
	case [2]string{"slices", "contains"}:
		return &Builtin{N: "bool"}
	case [2]string{"slices", "indexOf"}:
		return &Builtin{N: "int"}
	}
	// Value-handle constructors (binding.handleCtors): a package function that
	// builds a stdlib struct handle (regexp.mustCompile → regexp.Regexp). The
	// return spelling is the handle's Aril boundary type (VALUE-HANDLES).
	if hc, ok := binding.HandleCtorOf(pkg, method); ok {
		return semaTypeFromSpelling(hc.Return)
	}
	// Derived rows (D6): the binding registry carries the Aril return spelling
	// (e.g. `Result<int, error>`, `RecvChan<time.Time>`, `[]string`). An empty
	// spelling is a unit/effect binding (e.g. os.exit) — nil, as before.
	if spelling, ok := binding.ReturnSpelling(pkg, method); ok && spelling != "" {
		return semaTypeFromSpelling(spelling)
	}
	return nil
}

// semaTypeFromSpelling maps an Aril type spelling from the binding registry to
// a sema Type. The spellings are the canonical Aril forms bindgen's translate
// emits, over the construct set the stdlib registry uses: scalars, `[]T`,
// `Result<T, error>`, and the directional channels. A leaf carrying a `.` is a
// qualified opaque boundary type (`time.Time`) → Named; a bare leaf is a
// primitive → Builtin (reproducing the former hand-built representations).
func semaTypeFromSpelling(s string) Type {
	s = strings.TrimSpace(s)
	if elem, ok := strings.CutPrefix(s, "[]"); ok {
		return &Slice{Elem: semaTypeFromSpelling(elem)}
	}
	if args, ok := genericArgs(s, "Result"); ok && len(args) == 2 {
		return &Result{T: semaTypeFromSpelling(args[0]), E: semaTypeFromSpelling(args[1])}
	}
	if args, ok := genericArgs(s, "Option"); ok && len(args) == 1 {
		return &Option{T: semaTypeFromSpelling(args[0])}
	}
	if args, ok := genericArgs(s, "RecvChan"); ok && len(args) == 1 {
		return &RecvChan{Elem: semaTypeFromSpelling(args[0])}
	}
	if args, ok := genericArgs(s, "SendChan"); ok && len(args) == 1 {
		return &SendChan{Elem: semaTypeFromSpelling(args[0])}
	}
	if args, ok := genericArgs(s, "Channel"); ok && len(args) == 1 {
		return &Channel{Elem: semaTypeFromSpelling(args[0])}
	}
	if args, ok := genericArgs(s, "Map"); ok && len(args) == 2 {
		return &Map{Key: semaTypeFromSpelling(args[0]), Val: semaTypeFromSpelling(args[1])}
	}
	switch s {
	case "unit":
		return &Unit{}
	}
	if strings.Contains(s, ".") {
		return &Named{N: s} // a qualified opaque boundary type, e.g. time.Time
	}
	return &Builtin{N: s} // a primitive (int / string / byte / …) or `error`
}

// genericArgs returns the top-level type arguments of `ctor<...>` (commas at
// angle-bracket depth 0), or ok=false when s is not that constructor.
func genericArgs(s, ctor string) ([]string, bool) {
	if !strings.HasPrefix(s, ctor+"<") || !strings.HasSuffix(s, ">") {
		return nil, false
	}
	inner := s[len(ctor)+1 : len(s)-1]
	var args []string
	depth, start := 0, 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	return append(args, strings.TrimSpace(inner[start:])), true
}

// bindingCallReturn types a stdlib-binding call `recv.method(...)` whose
// receiver names a package (an Ident, not a local value). Returns nil
// when the callee is not a tabled binding.
func (c *checker) bindingCallReturn(f *ast.Field) Type {
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return nil
	}
	// The receiver must name an imported package (SymBuiltinModule),
	// not a value binding that happens to share the name — a local
	// shadowing the package must not be treated as a namespace.
	sym := c.info.Symbol[recv]
	if sym == nil || sym.Kind != SymBuiltinModule {
		return nil
	}
	return c.stdlibBindingReturn(recv.Name, f.Name)
}

// genericBindingReturn types the generic bindings whose result type
// depends on the *call's* type arguments rather than a fixed table row:
// `json.parse<T>(data)` → `Result<T, error>`, and the `fmt.scan` family —
// `scan<T>` → `Result<T, error>`, `scan2<A,B>` →
// `Result<(A,B), error>`, `scan3<A,B,C>` → `Result<(A,B,C), error>`
// (binding-surface.md §fmt). Without this a `try fmt.scan<int>()` left
// its binding Unknown, which cascaded into "cannot infer Go type for
// tuple literal" once the value reached a tuple component (p1242).
// Mirrors codegen's scan lowering (internal/codegen/call.go,
// isFmtScan / fmtScanMultiArity).
func (c *checker) genericBindingReturn(call *ast.Call, f *ast.Field) Type {
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return nil
	}
	if sym := c.info.Symbol[recv]; sym == nil || sym.Kind != SymBuiltinModule {
		return nil
	}
	errT := &Builtin{N: "error"}
	// json.parse<T>(data) → Result<T, error> — generic over the target
	// type, like the scan family (binding-surface.md §encoding/json).
	if recv.Name == "json" && f.Name == "parse" {
		if len(call.TypeArgs) != 1 {
			return nil
		}
		return &Result{T: c.typeFromExpr(call.TypeArgs[0]), E: errT}
	}
	if recv.Name != "fmt" {
		return nil
	}
	want := 0
	switch f.Name {
	case "scan":
		want = 1
	case "scan2":
		want = 2
	case "scan3":
		want = 3
	default:
		return nil
	}
	if len(call.TypeArgs) != want {
		return nil
	}
	if want == 1 {
		return &Result{T: c.typeFromExpr(call.TypeArgs[0]), E: errT}
	}
	comps := make([]Type, want)
	for i, ta := range call.TypeArgs {
		comps[i] = c.typeFromExpr(ta)
	}
	return &Result{T: &Tuple{Comps: comps}, E: errT}
}
