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
	c.contractEntrySyms = map[*ast.FuncDecl][]*Symbol{}
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

// isUnitReturnType reports whether a function return annotation carries no
// value: a nil annotation (implicit unit) or an explicit `unit`.
func isUnitReturnType(t ast.TypeExpr) bool {
	if t == nil {
		return true
	}
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "unit"
}

// resolveFuncContract binds the names in the current function's
// requires/ensures/entry obligations (RFC-0006). `requires` resolves in the
// param scope; an `entry { let … }` section's values resolve in the param
// scope and its names are declared in an entry scope; `ensures` resolves in
// the param scope extended with the entry names and `result` (the return
// value). Called from the resolve pass after the body is resolved.
func (c *checker) resolveFuncContract(fn *ast.FuncDecl, fnScope *Scope) {
	if c.curContract == nil {
		return
	}
	entryScope := newScope(fnScope)
	var entrySyms []*Symbol
	for _, cl := range c.curContract.Clauses {
		if cl.Kind != "entry" {
			continue
		}
		for _, bd := range cl.Bindings {
			c.resolveExpr(bd.Value, fnScope) // entry value sees params only
			sym := &Symbol{Name: bd.Name, Kind: SymLocal, Type: &Unknown{}}
			entryScope.declare(sym)
			entrySyms = append(entrySyms, sym)
		}
	}
	ensScope := newScope(entryScope)
	// `result` is in scope for `ensures` only on a value-returning function;
	// a unit-returning function has no result, so `result` there stays
	// unbound (E0103). An ensures over params / entry snapshots is still
	// legal and checked on such a function.
	if !isUnitReturnType(fn.ReturnType) {
		ensScope.declare(&Symbol{Name: "result", Kind: SymLocal, Type: c.typeFromExpr(fn.ReturnType)})
	}
	for _, cl := range c.curContract.Clauses {
		switch cl.Kind {
		case "requires":
			c.resolveExpr(cl.Pred, fnScope) // precondition: params only
		case "ensures":
			c.resolveExpr(cl.Pred, ensScope) // params + entry names + result
		}
	}
	if len(entrySyms) > 0 {
		c.contractEntrySyms[fn] = entrySyms
	}
}

// checkFuncContract infers the function's requires/ensures/entry predicates,
// requires each predicate to be `bool` (E1102), sets the entry-binding symbol
// types from the inferred values, and records the obligations on
// Info.FuncContracts for codegen. Called from the check pass.
func (c *checker) checkFuncContract(fn *ast.FuncDecl) {
	if c.curContract == nil {
		return
	}
	fc := &FuncContract{}
	// Entry bindings first, so their names carry concrete types into the
	// ensures inference regardless of source order.
	entrySyms := c.contractEntrySyms[fn]
	si := 0
	for _, cl := range c.curContract.Clauses {
		if cl.Kind != "entry" {
			continue
		}
		for _, bd := range cl.Bindings {
			t := c.inferExpr(bd.Value)
			if si < len(entrySyms) {
				entrySyms[si].Type = t
				si++
			}
			fc.Entries = append(fc.Entries, EntryBinding{Name: bd.Name, Value: bd.Value})
		}
	}
	for _, cl := range c.curContract.Clauses {
		switch cl.Kind {
		case "requires":
			c.requireBoolPredicate(cl.Pred)
			fc.Requires = append(fc.Requires, cl.Pred)
		case "ensures":
			c.requireBoolPredicate(cl.Pred)
			fc.Ensures = append(fc.Ensures, cl.Pred)
		}
	}
	if len(fc.Requires)+len(fc.Ensures)+len(fc.Entries) > 0 {
		c.info.FuncContracts[fn] = fc
	}
}

// requireBoolPredicate infers a predicate and reports E1102 if it is neither
// `bool` nor (already-failed) Unknown.
func (c *checker) requireBoolPredicate(pred ast.Expr) {
	t := c.inferExpr(pred)
	if !isBool(t) && !isUnknownType(t) {
		c.report("E1102", "contract predicate must be a `bool`, got "+t.String(), pred.NodeSpan())
	}
}
