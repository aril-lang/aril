package sema

import "github.com/aril-lang/aril/internal/ast"

// catch.go — the sema half of the `expr catch e { … }` control-flow form
// (grammar.ebnf §CatchExpr, desugaring.md §Catch). The parser desugars catch
// to a two-arm match tagged `FromCatch`; this enforces the two catch-specific
// invariants a plain match does not:
//
//   - E0410: the subject must be a Result (the handler binds its Err payload).
//   - E0409: the handler block must *diverge* — end in `return` / `os.exit` /
//     `panic` / `break` / `continue`, never fall through with a value. This is
//     the "no error path the programmer did not consciously write" invariant:
//     recovering-and-continuing with a substituted value is `unwrapOr`'s job
//     (the visible substitution), not catch's.
//
// The divergence predicate mirrors codegen's blockAlwaysReturns
// (internal/codegen/control_flow.go) — the two must stay in lockstep so sema
// rejects exactly what codegen cannot lower.

// checkCatch enforces the FromCatch invariants on a desugared catch match.
func (c *checker) checkCatch(m *ast.MatchExpr, subject Type) {
	if !m.FromCatch || len(m.Arms) != 2 {
		return
	}
	if _, ok := subject.(*Result); !ok {
		if !isUnknown(subject) {
			c.report("E0410", "`catch` requires a Result subject, got "+subject.String(), m.CatchKw)
		}
		return
	}
	// Arms[1] is the `Err(e) => <block>` handler, by parser construction.
	handler := m.Arms[1].Body
	blk, ok := handler.(*ast.Block)
	if !ok || !catchBlockDiverges(blk) {
		c.report("E0409",
			"a `catch` handler must diverge — end it with `return`, `os.exit`, or `panic`; "+
				"it cannot fall through with a value (use `unwrapOr` to substitute a value and continue)",
			handler.NodeSpan())
	}
}

// catchBlockDiverges reports whether a block guarantees control leaves the
// function (or loop): a trailing `return`/`break`/`continue`, an `os.exit` /
// `panic` call, or an `if`/`else` whose every branch diverges. Mirrors
// codegen's blockAlwaysReturns.
func catchBlockDiverges(b *ast.Block) bool {
	if b.Trailing != nil {
		return catchExprDiverges(b.Trailing)
	}
	if len(b.Stmts) == 0 {
		return false
	}
	return catchStmtDiverges(b.Stmts[len(b.Stmts)-1])
}

func catchExprDiverges(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return catchExprDiverges(v.Inner)
	case *ast.ReturnExpr, *ast.BreakExpr, *ast.ContinueExpr:
		return true
	case *ast.Call:
		if id, ok := v.Callee.(*ast.Ident); ok && id.Name == "panic" {
			return true
		}
		if f, ok := v.Callee.(*ast.Field); ok {
			if r, ok := f.Receiver.(*ast.Ident); ok && r.Name == "os" && f.Name == "exit" {
				return true
			}
		}
	case *ast.Block:
		return catchBlockDiverges(v)
	}
	return false
}

func catchStmtDiverges(s ast.Stmt) bool {
	switch v := s.(type) {
	case *ast.ExprStmt:
		return catchExprDiverges(v.Expr)
	case *ast.IfStmt:
		if v.Else == nil || !catchBlockDiverges(v.ThenBlock) {
			return false
		}
		switch e := v.Else.(type) {
		case *ast.Block:
			return catchBlockDiverges(e)
		case *ast.IfStmt:
			return catchStmtDiverges(e)
		}
	}
	return false
}
