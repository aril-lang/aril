package codegen

import (
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
	"github.com/aril-lang/aril/internal/sema"
)

func emitContract(t *testing.T, src, mode string) string {
	t.Helper()
	toks, lerr := lexer.LexFile(src, "c.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "c.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	files := []*ast.File{f}
	paths := []string{"c.aril"}
	info, diags := sema.CheckFiles(files, paths)
	if len(diags) > 0 {
		t.Fatalf("sema: %v", diags)
	}
	out, err := EmitFilesWithOptions(files, paths, info, Options{ContractMode: mode})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

// TestEmitLoopInvariant: under --contracts=panic a loop invariant lowers to a
// per-iteration `if !(pred) { panic(...) }`; under off nothing is emitted
// (byte-identical lowering).
func TestEmitLoopInvariant(t *testing.T) {
	src := `func run(xs: []int): int {
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}

contract run {
  loop scan {
    invariant acc >= 0
  }
}
`
	panicMode := emitContract(t, src, "panic")
	if !strings.Contains(panicMode, "if !(acc >= 0)") {
		t.Errorf("panic-mode emit missing the invariant check:\n%s", panicMode)
	}
	if !strings.Contains(panicMode, "loop invariant violated (loop scan)") {
		t.Errorf("panic-mode emit missing the blame message:\n%s", panicMode)
	}

	off := emitContract(t, src, "off")
	if strings.Contains(off, "loop invariant violated") {
		t.Errorf("off-mode emit must not contain a contract check:\n%s", off)
	}
	// off mode is byte-identical to the same program without the contract.
	noContract := emitContract(t, `func run(xs: []int): int {
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}
`, "off")
	if off != noContract {
		t.Errorf("off-mode emit differs from the contract-free program:\n--- with ---\n%s\n--- without ---\n%s", off, noContract)
	}
}

// TestEmitEnsures: an ensures lowers to a Go named-return + deferred
// re-entrancy-guarded post-check; entry bindings become entry temps; off
// emits nothing.
func TestEmitEnsures(t *testing.T) {
	src := `func dbl(x: int): int { return x + x }

contract dbl {
  entry { let x0 = x }
  ensures result == x0 + x0
}
`
	got := emitContract(t, src, "panic")
	for _, want := range []string{
		"(_arilRet int)",                          // named return
		"_arilEntry_x0 := ",                       // entry temp
		"defer func()",                            // deferred post-check
		"if r := recover()",                       // recover-rethrow guard
		"if !_arilInContract {",                   // re-entrancy guard
		"_arilRet == _arilEntry_x0+_arilEntry_x0", // result→_arilRet, entry subst
		"ensures violated (dbl)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panic-mode emit missing %q:\n%s", want, got)
		}
	}
	off := emitContract(t, src, "off")
	if strings.Contains(off, "_arilRet") || strings.Contains(off, "ensures violated") {
		t.Errorf("off-mode emit must not lower the contract:\n%s", off)
	}
}

// TestEmitTypeInvariant: a class invariant lowers to a method-exit `defer`
// guarded check on every non-static method (the predicate reading the
// post-mutation receiver `t.<field>`); a static method is not checked; off
// emits nothing.
func TestEmitTypeInvariant(t *testing.T) {
	src := `class Counter {
  var n: int
  static new(): Counter { return Counter{ n: 0 } }
  bump() { n = n + 1 }
}

contract Counter {
  invariant n >= 0
}
`
	got := emitContract(t, src, "panic")
	for _, want := range []string{
		"func (_arilSelf *Counter) bump()", // the mutating method
		"defer func()",                     // method-exit defer
		"if r := recover()",                // recover-rethrow guard
		"if !_arilInContract {",            // re-entrancy guard
		"_arilSelf.N >= 0",                 // bare field via implicit receiver
		"invariant violated (Counter)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panic-mode emit missing %q:\n%s", want, got)
		}
	}
	// The static factory has no receiver, so it carries no method-exit
	// `defer` check (its only invariant check is the construction IIFE around
	// the `Counter{…}` literal, which uses no defer — covered separately by
	// TestEmitConstructionInvariant).
	staticIdx := strings.Index(got, "func counterNew()")
	bumpIdx := strings.Index(got, "func (_arilSelf *Counter) bump()")
	if staticIdx < 0 || bumpIdx < 0 {
		t.Fatalf("expected both the static factory and bump in the emit:\n%s", got)
	}
	staticBody := got[staticIdx:bumpIdx]
	if strings.Contains(staticBody, "defer func()") {
		t.Errorf("static method must not carry a method-exit defer check:\n%s", staticBody)
	}

	off := emitContract(t, src, "off")
	if strings.Contains(off, "_arilInContract") || strings.Contains(off, "invariant violated") {
		t.Errorf("off-mode emit must not lower the invariant:\n%s", off)
	}
}

// TestEmitConstructionInvariant: a brace literal of an invariant-bearing type
// lowers inside a `func() T { _arilNew := <lit>; <check>; return _arilNew }()`
// so construction is validated. Covers the record case (a record's only
// checkpoint — no methods) and the off-mode byte-identical elision.
func TestEmitConstructionInvariant(t *testing.T) {
	// The contract block is placed last so removing it (the contract-free
	// program below) does not shift main's source lines — the `//line`
	// directives then match, making the off-mode byte-identity check exact.
	src := `type Interval = {
  start: int
  end:   int
}

func main() {
  let v = Interval{ start: 1, end: 5 }
}

contract Interval {
  invariant start <= end
}
`
	got := emitContract(t, src, "panic")
	for _, want := range []string{
		"func() Interval {",              // construction IIFE (record = value type, no *)
		"_arilNew := Interval{Start: 1",  // the literal bound to the temp
		"_arilNew.Start <= _arilNew.End", // fields via the construction temp
		"invariant violated (Interval)",  // blame names the type
		"return _arilNew",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panic-mode emit missing %q:\n%s", want, got)
		}
	}

	off := emitContract(t, src, "off")
	if strings.Contains(off, "_arilNew") || strings.Contains(off, "invariant violated") {
		t.Errorf("off-mode emit must not lower the construction check:\n%s", off)
	}
	// off mode is byte-identical to the same program without the contract.
	noContract := emitContract(t, `type Interval = {
  start: int
  end:   int
}

func main() {
  let v = Interval{ start: 1, end: 5 }
}
`, "off")
	if off != noContract {
		t.Errorf("off-mode emit differs from the contract-free program:\n--- with ---\n%s\n--- without ---\n%s", off, noContract)
	}
}
