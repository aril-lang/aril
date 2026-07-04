package ast

import "testing"

// Direct coverage for the shared divergence predicate — the single source of
// truth that sema's checkCatch (E0409) and codegen's value-position lowering
// both consume, so a drift here silently breaks their contract. Before this
// file the logic was exercised only transitively (codegen goldens +
// sema/catch_test.go); these lock the exported entry points against drift.

func call(callee Expr) *Call        { return &Call{Callee: callee} }
func ident(n string) *Ident         { return &Ident{Name: n} }
func exprStmt(e Expr) *ExprStmt     { return &ExprStmt{Expr: e} }
func fieldOf(recv, n string) *Field { return &Field{Receiver: ident(recv), Name: n} }
func blockTrailing(e Expr) *Block   { return &Block{Trailing: e} }
func blockStmts(ss ...Stmt) *Block  { return &Block{Stmts: ss} }

func TestExprDiverges(t *testing.T) {
	cases := []struct {
		name string
		e    Expr
		want bool
	}{
		{"return", &ReturnExpr{}, true},
		{"break", &BreakExpr{}, true},
		{"continue", &ContinueExpr{}, true},
		{"paren wraps return", &ParenExpr{Inner: &ReturnExpr{}}, true},
		{"paren wraps plain", &ParenExpr{Inner: ident("x")}, false},
		{"panic call", call(ident("panic")), true},
		{"os.exit call", call(fieldOf("os", "exit")), true},
		{"foo.exit is not os.exit", call(fieldOf("foo", "exit")), false},
		{"os.other is not exit", call(fieldOf("os", "other")), false},
		{"plain ident call", call(ident("f")), false},
		{"plain ident", ident("x"), false},
		{"block ending in return", blockStmts(exprStmt(&ReturnExpr{})), true},
		{"block ending in plain stmt", blockStmts(exprStmt(ident("x"))), false},
	}
	for _, c := range cases {
		if got := ExprDiverges(c.e); got != c.want {
			t.Errorf("%s: ExprDiverges = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBlockDiverges(t *testing.T) {
	// if/else where every branch diverges → the block diverges; if either
	// branch (or the else) can fall through, it does not.
	divergingIf := &IfStmt{
		Cond:      ident("flag"),
		ThenBlock: blockTrailing(&ReturnExpr{}),
		Else:      blockTrailing(&ReturnExpr{}),
	}
	thenFallsThrough := &IfStmt{
		Cond:      ident("flag"),
		ThenBlock: blockTrailing(ident("x")),
		Else:      blockTrailing(&ReturnExpr{}),
	}
	noElse := &IfStmt{Cond: ident("flag"), ThenBlock: blockTrailing(&ReturnExpr{})}
	nestedElseIf := &IfStmt{
		Cond:      ident("a"),
		ThenBlock: blockTrailing(&ReturnExpr{}),
		Else: &IfStmt{
			Cond:      ident("b"),
			ThenBlock: blockTrailing(&ReturnExpr{}),
			Else:      blockTrailing(&ReturnExpr{}),
		},
	}

	cases := []struct {
		name string
		b    *Block
		want bool
	}{
		{"empty block", blockStmts(), false},
		{"trailing return", blockTrailing(&ReturnExpr{}), true},
		{"trailing plain", blockTrailing(ident("x")), false},
		{"last stmt returns", blockStmts(exprStmt(ident("x")), exprStmt(&ReturnExpr{})), true},
		{"last stmt falls through", blockStmts(exprStmt(&ReturnExpr{}), exprStmt(ident("x"))), false},
		{"if/else both diverge", blockStmts(divergingIf), true},
		{"if then falls through", blockStmts(thenFallsThrough), false},
		{"if without else", blockStmts(noElse), false},
		{"nested else-if all diverge", blockStmts(nestedElseIf), true},
	}
	for _, c := range cases {
		if got := BlockDiverges(c.b); got != c.want {
			t.Errorf("%s: BlockDiverges = %v, want %v", c.name, got, c.want)
		}
	}
}
