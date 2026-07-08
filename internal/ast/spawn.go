package ast

// Scope-join placement query — a pure structural walk answering "before which
// statement of this scope body must the join (`Group.Wait`) be emitted?".
//
// The consumer is codegen's ScopeIR lowering (lowering-go.md §ScopeIR). A scope
// that collects its spawns' results into a channel and *returns* them must drain
// that channel in-body — the in-scope fan-in idiom:
//
//	scope<[]T, error> { …spawns send to ch…; ch.close(); for x in ch {…}; out }
//
// The fan-in `ch.close()` must run only after every sibling spawn has finished
// sending, so the join belongs *immediately before* that close. Emitting the
// join after the whole body (the naive placement) closes the channel while a
// sibling is still sending — send-after-close, E1203.
//
// The distinguishing signal is the top-level `close()` itself. The *other*
// in-scope pattern — direct concurrent receives (`let a = ch.recv()`) that must
// interleave with the still-running sends over an unbuffered channel — has no
// such close, and its recvs must stay *before* the join (moving the join ahead of
// them would deadlock: Wait blocks on sends nobody is receiving yet). So: a
// top-level fan-in close after the spawns ⇒ join before it; otherwise ⇒ join at
// the body end (the concurrent-recv and external-drain scopes, byte-identical to
// the pre-fix lowering). The fan-in channel must be buffered ≥ spawn count, or
// the post-join drain's producers would have blocked before the join could return
// (§ScopeIR).
//
// The walk STOPS at a nested `ScopeExpr`: a spawn inside an inner scope registers
// on the inner group and is joined by the inner scope's own `Wait`, so it is
// invisible to the enclosing scope's placement.

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
