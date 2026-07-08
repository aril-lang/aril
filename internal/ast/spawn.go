package ast

// Scope-join placement — the structural query codegen's ScopeIR lowering uses to
// put `Group.Wait` before an in-scope fan-in `close()`, so the drain does not race
// the spawns (send-after-close, E1203). Rationale + the buffered-fan-in constraint
// live in lowering-go.md §ScopeIR.
//
// Invariant (not obvious from the code): the walk STOPS at a nested `ScopeExpr` —
// a spawn there registers on the inner group and is joined by the inner scope's
// own `Wait`, so it is invisible to this scope's placement.

// ScopeJoinBeforeIndex reports the index of the statement the join must precede
// (the fan-in `close()` after the last own spawn), or -1 to place the join after
// the whole body. -1 covers a scope with no own spawn, a concurrent-recv scope
// (no in-body fan-in close), and an external-drain scope — all keep the pre-fix
// join-at-end placement.
func ScopeJoinBeforeIndex(b *Block) int {
	lastSpawn := -1
	for i, s := range b.Stmts {
		if stmtLaunchesSpawn(s) {
			lastSpawn = i
		}
	}
	if lastSpawn < 0 {
		return -1
	}
	for i := lastSpawn + 1; i < len(b.Stmts); i++ {
		if stmtIsChannelClose(b.Stmts[i]) {
			return i
		}
	}
	return -1
}

// stmtIsChannelClose reports whether s is a top-level `<ident>.close()` call
// statement — the fan-in close shape (`itemCh.close()`). A named-channel receiver
// keeps it precise: the corpus fan-in close is always `<chan>.close()` with no
// arguments.
func stmtIsChannelClose(s Stmt) bool {
	es, ok := s.(*ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.Expr.(*Call)
	if !ok || len(call.Args) != 0 {
		return false
	}
	f, ok := call.Callee.(*Field)
	if !ok || f.Name != "close" {
		return false
	}
	_, ok = f.Receiver.(*Ident)
	return ok
}

// stmtLaunchesSpawn reports whether s launches a spawn in the current scope
// (descending through control flow, stopping at nested scope boundaries).
func stmtLaunchesSpawn(s Stmt) bool {
	switch v := s.(type) {
	case *ExprStmt:
		return exprLaunchesSpawn(v.Expr)
	case *LetStmt:
		return v.Value != nil && exprLaunchesSpawn(v.Value)
	case *VarStmt:
		return v.Value != nil && exprLaunchesSpawn(v.Value)
	case *AssignStmt:
		return exprLaunchesSpawn(v.Value)
	case *ForStmt:
		return v.Body != nil && blockLaunchesSpawn(v.Body)
	case *WhileStmt:
		return v.Body != nil && blockLaunchesSpawn(v.Body)
	case *IfStmt:
		if v.ThenBlock != nil && blockLaunchesSpawn(v.ThenBlock) {
			return true
		}
		switch e := v.Else.(type) {
		case *Block:
			return blockLaunchesSpawn(e)
		case *IfStmt:
			return stmtLaunchesSpawn(e)
		}
	case *SelectStmt:
		for _, c := range v.Cases {
			if selectCaseLaunchesSpawn(c) {
				return true
			}
		}
	}
	return false
}

func selectCaseLaunchesSpawn(c SelectCase) bool {
	switch v := c.(type) {
	case *SelectRecv:
		return v.Body != nil && blockLaunchesSpawn(v.Body)
	case *SelectSend:
		return v.Body != nil && blockLaunchesSpawn(v.Body)
	case *SelectDefault:
		return v.Body != nil && blockLaunchesSpawn(v.Body)
	}
	return false
}

func blockLaunchesSpawn(b *Block) bool {
	for _, s := range b.Stmts {
		if stmtLaunchesSpawn(s) {
			return true
		}
	}
	return b.Trailing != nil && exprLaunchesSpawn(b.Trailing)
}

// exprLaunchesSpawn reports whether e is (or structurally contains, within the
// current scope) a `spawn`. A nested `ScopeExpr` is a hard boundary — its spawns
// belong to the inner group — so the walk does not descend into it.
func exprLaunchesSpawn(e Expr) bool {
	switch v := e.(type) {
	case *SpawnExpr:
		return true
	case *ParenExpr:
		return exprLaunchesSpawn(v.Inner)
	case *Block:
		return blockLaunchesSpawn(v)
	case *IfExpr:
		if v.ThenBlock != nil && blockLaunchesSpawn(v.ThenBlock) {
			return true
		}
		switch e := v.Else.(type) {
		case *Block:
			return blockLaunchesSpawn(e)
		case *IfExpr:
			return exprLaunchesSpawn(e)
		}
	case *MatchExpr:
		for _, arm := range v.Arms {
			if arm.Body != nil && exprLaunchesSpawn(arm.Body) {
				return true
			}
		}
	case *ScopeExpr:
		// Boundary: the inner scope joins its own spawns.
		return false
	}
	return false
}
