package codegen

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
)

// catch.go — lowering for the `expr catch e { … }` control-flow form
// (desugaring.md §Catch). The parser desugars catch to a two-arm match tagged
// FromCatch (`{ Ok(__aril_catch_v) => __aril_catch_v, Err(e) => <block> }`); at
// value/return position codegen must NOT reuse the match value-lowering, which
// wraps the arms in an IIFE (`func() T { … }()`) — that would trap the
// handler's `return` inside the IIFE instead of returning from the enclosing
// function, the whole point of catch (forced return of the enclosing frame,
// per our write-time-honesty invariant). Instead catch lowers exactly like
// `try` (try.go): a statement preamble + an `<tmp>.V` value, so the handler
// block runs in the function's own frame and its `return`/`os.exit` do the
// right thing. Bare statement-position catch keeps the plain switch
// (emitMatchAsStmt) — no IIFE there, so its `return` already escapes.

// emitBailPreamble emits the shared statement preamble for a *bail form* — a
// `try e` (early-return on Err/None) or an `e catch err { … }` (custom
// diverging handler). Both lower to a preamble that binds a fresh temp and
// conditionally leaves the frame; the caller then reads the unwrapped Ok
// payload via `<tmp>.V`. isBail is false (with an empty tmp) when value is
// neither, so the caller falls through to its ordinary emission path.
//
// This folds the three positions where `try` and `catch` were threaded as
// parallel branch-pairs differing only in which preamble ran (return value,
// `let`/`var` initializer, discarded expression statement); a future bail form
// (e.g. a pattern-catch shorthand) extends the switch here, not all three.
func (g *gen) emitBailPreamble(value ast.Expr) (tmp string, isBail bool, err error) {
	switch v := value.(type) {
	case *ast.TryExpr:
		tmp, err = g.emitTryPreamble(v)
		return tmp, true, err
	case *ast.MatchExpr:
		if v.FromCatch {
			tmp, err = g.emitCatchPreamble(v)
			return tmp, true, err
		}
	}
	return "", false, nil
}

// emitCatchPreamble lowers a FromCatch match to the try-style preamble:
//
//	<tmp> := <subject>
//	if <tmp>.Tag == 1 {   // Err
//	    e := <tmp>.E       // (+ `_ = e` when unused)
//	    <handler block>    // required to diverge (sema E0409)
//	}
//
// and returns <tmp>; the caller reads the unwrapped Ok payload via `<tmp>.V`.
// The handler statements are emitted directly in the current frame (not an
// IIFE), so a `return` inside returns from the enclosing function.
func (g *gen) emitCatchPreamble(m *ast.MatchExpr) (string, error) {
	errArm := m.Arms[1] // `Err(e) => <block>`, by parser construction
	vp, ok := errArm.Pattern.(*ast.VariantPat)
	if !ok {
		return "", fmt.Errorf("codegen: malformed catch (Err arm pattern %T)", errArm.Pattern)
	}
	block, ok := errArm.Body.(*ast.Block)
	if !ok {
		return "", fmt.Errorf("codegen: malformed catch (handler body %T, want block)", errArm.Body)
	}
	// A nested `try` in the subject is hoisted first (mirrors emitTryPreamble),
	// when reordering is safe.
	if err := g.hoistTriesIfSafe(m.Subject); err != nil {
		return "", err
	}
	g.tryTempCounter++
	tmp := fmt.Sprintf("__aril_catch_%d", g.tryTempCounter)
	g.line(m.Span.StartLine)
	g.writeIndent()
	g.b.WriteString(tmp)
	g.b.WriteString(" := ")
	if err := g.emitExpr(m.Subject); err != nil {
		return "", err
	}
	g.b.WriteByte('\n')
	g.writeIndent()
	g.b.WriteString("if ")
	g.b.WriteString(tmp)
	g.b.WriteString(".Tag == 1 {\n") // Err
	g.indent++
	// `e := <tmp>.E` (+ the unused-binding guard, reused from match lowering).
	if err := g.emitPayloadBindings(vp, tmp); err != nil {
		return "", err
	}
	for _, s := range block.Stmts {
		if err := g.emitStmt(s); err != nil {
			return "", err
		}
	}
	if block.Trailing != nil {
		// A trailing diverging expr (e.g. `catch e { os.exit(1) }`) — emit it
		// as a statement (it never produces a value).
		if err := g.emitStmt(&ast.ExprStmt{Span: block.Trailing.NodeSpan(), Expr: block.Trailing}); err != nil {
			return "", err
		}
	}
	g.indent--
	g.writeIndent()
	g.b.WriteString("}\n")
	return tmp, nil
}

// catchExprErr is emitted when a FromCatch match reaches the value-position
// match lowering (emitMatchAsExpr) — i.e. catch was used in a deep expression
// position (call argument, operator operand, …) rather than bound to a
// `let`/`var` or used as a `return` value, the positions emitCatchPreamble
// handles. A graceful limitation (never a miscompile), mirroring tryExprErr.
func catchExprErr() error {
	return fmt.Errorf("codegen: `catch` in this expression position is not supported — bind it to a `let`/`var` (`let x = e catch err { … }`) or use it as a `return` value")
}
