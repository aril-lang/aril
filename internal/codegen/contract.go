package codegen

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/sema"
)

// Contract lowering (RFC-0006). Mirrors the sema side (sema/contract.go):
// loop invariants, requires/ensures/entry obligations, and the re-entrancy
// guard are emitted here, keyed off sema's Info.LoopInvariants /
// Info.FuncContracts. Everything is gated on `--contracts=panic`; under off
// (the default) nothing is emitted — byte-identical lowering.

// emitLoopInvariants lowers a labelled loop's contract invariants (RFC-0006)
// to a per-iteration check, emitted at the end of the loop body so it runs
// after every iteration. Under `--contracts=panic` a violated invariant
// aborts; the `//line` directive at the predicate maps the panic back to the
// `.aril` source (blame in Aril coordinates, D10). Under off (the default)
// nothing is emitted — byte-identical lowering. Keyed by the loop node on
// sema's Info.LoopInvariants (populated only for a labelled loop with a
// matching `loop <label>` section).
func (g *gen) emitLoopInvariants(loop ast.Stmt) error {
	if g.contractMode != "panic" || g.info == nil {
		return nil
	}
	preds := g.info.LoopInvariants[loop]
	if len(preds) == 0 {
		return nil
	}
	label := loopLabel(loop)
	for _, pred := range preds {
		g.line(pred.NodeSpan().StartLine)
		g.writeIndent()
		g.b.WriteString("if !(")
		if err := g.emitExpr(pred); err != nil {
			return err
		}
		g.b.WriteString(") {\n")
		g.indent++
		g.writeIndent()
		g.b.WriteString(fmt.Sprintf("panic(%q)\n",
			"aril: contract: loop invariant violated (loop "+label+")"))
		g.indent--
		g.writeIndent()
		g.b.WriteString("}\n")
	}
	return nil
}

// contractFor returns the function's resolved contract obligations under
// panic mode (nil otherwise — off elides everything).
func (g *gen) contractFor(fn *ast.FuncDecl) *sema.FuncContract {
	if g.contractMode != "panic" || g.info == nil {
		return nil
	}
	return g.info.FuncContracts[fn]
}

// emitContractPrologue lowers a function's requires/ensures/entry obligations
// (RFC-0006, panic mode), emitted at the top of the body:
//   - each `entry { let n = e }` → an entry temp `_arilEntry_n := <e>`;
//   - each `requires P` → an entry check `if !(P) { panic(…) }`;
//   - the `ensures` → a deferred post-check that, on normal return (guarded
//     by recover-rethrow so a panic-in-progress is not masked), asserts each
//     predicate against the named return `_arilRet` and the entry temps.
func (g *gen) emitContractPrologue(fc *sema.FuncContract, fn *ast.FuncDecl, named bool) error {
	entryVars := map[string]string{}
	for _, e := range fc.Entries {
		name := "_arilEntry_" + goIdent(e.Name)
		entryVars[e.Name] = name
		g.line(e.Value.NodeSpan().StartLine)
		g.writeIndent()
		g.b.WriteString(name)
		g.b.WriteString(" := ")
		if err := g.emitExpr(e.Value); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		// Suppress "declared and not used" when no ensures references it.
		g.writeIndent()
		g.b.WriteString("_ = ")
		g.b.WriteString(name)
		g.b.WriteByte('\n')
	}
	for _, req := range fc.Requires {
		if err := g.emitContractCheck(req, "requires", fn.Name); err != nil {
			return err
		}
	}
	if len(fc.Ensures) > 0 {
		// `result` lowers to the named return only on a value-returning
		// function; a unit function's ensures references params / entry
		// snapshots, never result (sema leaves it unbound there).
		if named {
			g.contractResultVar = "_arilRet"
		}
		g.contractEntryVars = entryVars
		defer func() { g.contractResultVar, g.contractEntryVars = "", nil }()
		if err := g.emitDeferredCheckBlock(fc.Ensures, "ensures", fn.Name); err != nil {
			return err
		}
	}
	return nil
}

// emitDeferredCheckBlock emits a `defer func() { … }()` that runs the predicate
// checks at every return path. The recover-rethrow preamble keeps a
// panic-in-progress from being masked, so the checks (and any contract
// violation) only fire on a *normal* return. Each predicate runs through the
// re-entrancy-guarded check (`emitContractCheck`). Shared by the `ensures`
// post-check and the method-exit type-invariant check — both are deferred
// every-return-path checks with the identical wrapper.
func (g *gen) emitDeferredCheckBlock(preds []ast.Expr, kind, name string) error {
	g.writeIndent()
	g.b.WriteString("defer func() {\n")
	g.indent++
	g.writeIndent()
	g.b.WriteString("if r := recover(); r != nil {\n")
	g.indent++
	g.writeIndent()
	g.b.WriteString("panic(r)\n")
	g.indent--
	g.writeIndent()
	g.b.WriteString("}\n")
	for _, pred := range preds {
		if err := g.emitContractCheck(pred, kind, name); err != nil {
			return err
		}
	}
	g.indent--
	g.writeIndent()
	g.b.WriteString("}()\n")
	return nil
}

// emitContractCheck emits a guarded predicate check:
//
//	if !_arilInContract {
//	    _arilInContract = true
//	    _arilPass := (<pred>)
//	    _arilInContract = false
//	    if !_arilPass { panic("…kind violated (fn)") }
//	}
//
// The `_arilInContract` guard makes a predicate that calls the contracted
// function (or a mutually-contracted one) skip *its* contract during the
// evaluation, breaking the otherwise-unbounded recursion. The `//line` at the
// predicate maps blame to Aril coordinates (D10).
func (g *gen) emitContractCheck(pred ast.Expr, kind, fnName string) error {
	g.writeIndent()
	g.b.WriteString("if !_arilInContract {\n")
	g.indent++
	g.writeIndent()
	g.b.WriteString("_arilInContract = true\n")
	g.line(pred.NodeSpan().StartLine)
	g.writeIndent()
	g.b.WriteString("_arilPass := (")
	if err := g.emitExpr(pred); err != nil {
		return err
	}
	g.b.WriteString(")\n")
	g.writeIndent()
	g.b.WriteString("_arilInContract = false\n")
	g.writeIndent()
	g.b.WriteString("if !_arilPass {\n")
	g.indent++
	g.writeIndent()
	g.b.WriteString(fmt.Sprintf("panic(%q)\n", "aril: contract: "+kind+" violated ("+fnName+")"))
	g.indent--
	g.writeIndent()
	g.b.WriteString("}\n")
	g.indent--
	g.writeIndent()
	g.b.WriteString("}\n")
	return nil
}

// emitMethodInvariants lowers a class's type invariants (RFC-0006) to a
// method-exit check, emitted as a `defer` at the top of every non-static
// method body so it runs on each return path (the mutation boundary), via the
// shared emitDeferredCheckBlock wrapper. The invariant's field names lower to
// `t.<field>` (implicit receiver), so the check reads the post-mutation state.
// Under off (the default) nothing is emitted — byte-identical lowering. A
// static method (no receiver) is skipped here; construction-time checking is a
// later slice.
func (g *gen) emitMethodInvariants(className string) error {
	if g.contractMode != "panic" || g.info == nil {
		return nil
	}
	preds := g.info.TypeInvariants[className]
	if len(preds) == 0 {
		return nil
	}
	return g.emitDeferredCheckBlock(preds, "invariant", className)
}

// constructionInvariants returns the type invariants to check at a brace
// literal of the named type (nil unless --contracts=panic and the type
// carries invariants). The construction site is the only checkpoint for a
// record (no methods); for a class it complements the method-exit checks,
// catching an object that is built but never subsequently method-called.
func (g *gen) constructionInvariants(name string) []ast.Expr {
	if g.contractMode != "panic" || g.info == nil {
		return nil
	}
	return g.info.TypeInvariants[name]
}

// emitConstructionInvariants emits the guarded invariant checks for a
// construction temp (the brace literal already bound to `_arilNew`), reading
// the freshly-built value's fields. The predicate's field names lower against
// the construction temp via contractReceiver.
func (g *gen) emitConstructionInvariants(name string, preds []ast.Expr) error {
	prev := g.contractReceiver
	g.contractReceiver = "_arilNew"
	defer func() { g.contractReceiver = prev }()
	for _, pred := range preds {
		if err := g.emitContractCheck(pred, "invariant", name); err != nil {
			return err
		}
	}
	return nil
}

// loopLabel returns the contract label of a for/while loop ("" if none).
func loopLabel(loop ast.Stmt) string {
	switch v := loop.(type) {
	case *ast.ForStmt:
		return v.Label
	case *ast.WhileStmt:
		return v.Label
	}
	return ""
}
