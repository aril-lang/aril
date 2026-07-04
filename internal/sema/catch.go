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
// The divergence check delegates to the shared pure-AST predicate
// ast.BlockDiverges (internal/ast/diverge.go) — the single source of truth so
// sema rejects exactly what codegen cannot lower (they must not drift).

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
	if !ok || !ast.BlockDiverges(blk) {
		c.report("E0409",
			"a `catch` handler must diverge — end it with `return`, `os.exit`, or `panic`; "+
				"it cannot fall through with a value (use `unwrapOr` to substitute a value and continue)",
			handler.NodeSpan())
	}
}
