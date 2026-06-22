package sema

import "github.com/aril-lang/aril/internal/ast"

// Contract checking (RFC-0006). Separable contract blocks live in
// File.Contracts (kept out of Decls). They are checked here, in sema,
// reusing the package scope and the per-body environments — a predicate is
// ordinary Aril, so it goes through the same resolve + infer machinery.
//
// This first slice handles **loop invariants** (the early/localized
// diagnosis that is the point of contracts): a `loop <label>` section binds
// to the like-named labelled loop in the target's body, its predicates
// resolve in that loop's scope and must be `bool`, and codegen lowers each
// to a per-iteration check. requires/ensures/old/result and type invariants
// follow in later slices.

// indexContracts builds the target→ContractDecl map across all files and
// validates that each contract attaches to a known declaration (E1101) with
// well-formed loop-section labels.
func (c *checker) indexContracts(files []*ast.File, paths []string, scope *Scope) {
	c.contractByTarget = map[string]*ast.ContractDecl{}
	for i, f := range files {
		c.file = paths[i]
		for _, cd := range f.Contracts {
			sym := scope.lookup(cd.Target)
			if sym == nil || !isContractTarget(sym.Kind) {
				c.report("E1101", "contract `"+cd.Target+"` attaches to no such declaration", cd.Span)
				continue
			}
			c.contractByTarget[cd.Target] = cd
		}
	}
}

// isContractTarget reports whether a symbol kind can carry a contract.
func isContractTarget(k SymKind) bool {
	return k == SymFunc || k == SymTypeDecl || k == SymClass || k == SymInterface
}

// resolveLoopInvariants binds the names in the matching loop section's
// invariant predicates against `scope` — the loop's body scope, where the
// loop variable and the enclosing locals are visible. Called from the
// resolve pass at every labelled loop, which records the label as matched so
// reportUnmatchedLoopSections can flag a section naming a loop that does not
// exist. Driving this off the resolve walk (rather than a separate collector)
// means every loop is seen — including those nested in match / scope / select
// bodies — so no real loop is mistaken for missing.
func (c *checker) resolveLoopInvariants(label string, scope *Scope) {
	if c.curContract == nil || label == "" {
		return
	}
	for _, cl := range c.curContract.Clauses {
		if cl.Kind == "loop" && cl.Label == label {
			c.matchedLoopLabels[label] = true
			for _, inv := range cl.Loop {
				c.resolveExpr(inv.Pred, scope)
			}
		}
	}
}

// reportUnmatchedLoopSections flags each `loop <label>` section of the current
// contract whose label was not matched by any labelled loop in the body
// (E1101). Called after the target function's body has been resolved.
func (c *checker) reportUnmatchedLoopSections() {
	if c.curContract == nil {
		return
	}
	for _, cl := range c.curContract.Clauses {
		if cl.Kind == "loop" && !c.matchedLoopLabels[cl.Label] {
			c.report("E1101",
				"contract loop section names `"+cl.Label+"` but the target has no `loop "+cl.Label+"`",
				cl.Span)
		}
	}
}

// checkLoopInvariants infers each matching invariant predicate, requires it
// to be `bool` (E1102), and records it on Info.LoopInvariants for codegen.
// Called from the check pass at the labelled loop.
func (c *checker) checkLoopInvariants(loop ast.Stmt, label string) {
	if c.curContract == nil || label == "" {
		return
	}
	for _, cl := range c.curContract.Clauses {
		if cl.Kind != "loop" || cl.Label != label {
			continue
		}
		for _, inv := range cl.Loop {
			t := c.inferExpr(inv.Pred)
			if !isBool(t) && !isUnknownType(t) {
				c.report("E1102",
					"contract invariant must be a `bool`, got "+t.String(),
					inv.Pred.NodeSpan())
			}
			c.info.LoopInvariants[loop] = append(c.info.LoopInvariants[loop], inv.Pred)
		}
	}
}

// isUnknownType reports whether t is the conservative wildcard — used to
// suppress a cascading E1102 when the predicate already failed to type.
func isUnknownType(t Type) bool {
	_, ok := t.(*Unknown)
	return ok
}
