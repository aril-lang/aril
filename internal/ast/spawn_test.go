package ast

import "testing"

// Direct coverage for ScopeJoinBeforeIndex — the structural query codegen's
// ScopeIR lowering consumes to place the structured-scope join (Group.Wait)
// before an in-scope fan-in close, so the drain does not race the spawns
// (send-after-close, E1203). A drift here re-introduces the join-ordering bug
// that stranded nested_scopes at run-fail, so these lock the placement.

func spawnStmt() *ExprStmt          { return exprStmt(&SpawnExpr{Body: &Block{}}) }
func closeStmt(ch string) *ExprStmt { return exprStmt(&Call{Callee: fieldOf(ch, "close")}) }
func letStmt() *LetStmt             { return &LetStmt{} }

func TestScopeJoinBeforeIndex(t *testing.T) {
	cases := []struct {
		name string
		b    *Block
		want int
	}{
		// No spawn at all → join at end (degenerate scope).
		{"no spawn", blockStmts(letStmt(), closeStmt("ch")), -1},
		// External-drain shape: spawn is the last statement, nothing follows.
		{"spawn last", blockStmts(letStmt(), spawnStmt()), -1},
		// Concurrent-recv shape: spawns then direct recvs, NO close → join at
		// end so the recvs stay concurrent with the still-running sends.
		{"concurrent recv, no close", blockStmts(spawnStmt(), spawnStmt(), letStmt(), letStmt()), -1},
		// In-scope fan-in drain: the close after the spawns is the join point.
		{"fan-in close", blockStmts(letStmt(), spawnStmt(), closeStmt("ch"), letStmt()), 2},
		// Spawn nested in a for-loop body still counts; the trailing close is
		// the boundary.
		{"spawn in for-loop", blockStmts(
			letStmt(),
			&ForStmt{Body: blockStmts(spawnStmt())},
			closeStmt("ch"),
		), 2},
		// A close *before* the spawns is not a fan-in close for them.
		{"close before spawn", blockStmts(closeStmt("ch"), spawnStmt()), -1},
		// A nested scope's spawn is invisible (it joins on the inner group);
		// with no own spawn here the outer join stays at the end.
		{"nested-scope spawn is not own", blockStmts(
			exprStmt(&ScopeExpr{Body: blockStmts(spawnStmt())}),
			closeStmt("ch"),
		), -1},
		// First fan-in close after the spawns wins (two closes).
		{"first close after spawn wins", blockStmts(
			spawnStmt(), closeStmt("a"), closeStmt("b"),
		), 1},
	}
	for _, c := range cases {
		if got := ScopeJoinBeforeIndex(c.b); got != c.want {
			t.Errorf("%s: ScopeJoinBeforeIndex = %d, want %d", c.name, got, c.want)
		}
	}
}
