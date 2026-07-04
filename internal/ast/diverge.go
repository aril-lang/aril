package ast

// Divergence predicate — a pure structural query over the AST: does a
// block/expression guarantee that control *leaves* the enclosing function or
// loop (never falling through with a value)?
//
// Two consumers share this single source of truth, and MUST agree byte-for-byte
// or the sema↔codegen `catch` contract silently breaks:
//
//   - sema (checkCatch) rejects a `catch` handler that can fall through with a
//     value (E0409) — the "no error path the programmer did not consciously
//     write" invariant.
//   - codegen admits a value-position block/arm that ends in a diverging
//     statement (a `catch` handler, a block-body closure `=> { …; return x }`,
//     a diverging match arm) *without* a trailing expression — there is no Go
//     value to yield, so it emits the statements directly rather than an IIFE.
//
// A diverging construct is a trailing `return`/`break`/`continue`, a `panic(…)`
// or `os.exit(…)` call (both Never-typed per builtins.md §panic /
// binding-surface.md §os), or an `if`/`else` whose every branch diverges.

// BlockDiverges reports whether every control path through b leaves via a
// diverging statement, so the block yields no fall-through value.
func BlockDiverges(b *Block) bool {
	if b.Trailing != nil {
		return ExprDiverges(b.Trailing)
	}
	if len(b.Stmts) == 0 {
		return false
	}
	return stmtDiverges(b.Stmts[len(b.Stmts)-1])
}

// ExprDiverges reports whether e never produces a value.
func ExprDiverges(e Expr) bool {
	switch v := e.(type) {
	case *ParenExpr:
		return ExprDiverges(v.Inner)
	case *ReturnExpr, *BreakExpr, *ContinueExpr:
		return true
	case *Call:
		if id, ok := v.Callee.(*Ident); ok && id.Name == "panic" {
			return true
		}
		if f, ok := v.Callee.(*Field); ok && f.Name == "exit" {
			r, ok := f.Receiver.(*Ident)
			return ok && r.Name == "os"
		}
	case *Block:
		return BlockDiverges(v)
	}
	return false
}

// stmtDiverges reports whether s guarantees control leaves the enclosing
// block — a diverging expression statement, or an `if`/`else` whose every
// branch diverges.
func stmtDiverges(s Stmt) bool {
	switch v := s.(type) {
	case *ExprStmt:
		return ExprDiverges(v.Expr)
	case *IfStmt:
		if v.Else == nil || !BlockDiverges(v.ThenBlock) {
			return false
		}
		switch e := v.Else.(type) {
		case *Block:
			return BlockDiverges(e)
		case *IfStmt:
			return stmtDiverges(e)
		}
	}
	return false
}
