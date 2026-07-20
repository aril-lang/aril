package sema

import (
	"github.com/aril-lang/aril/internal/ast"
)

// resolveFile — name resolution over decls + bodies. Phase 1.
// See docs/internals/sema.md §4.
func (c *checker) resolveFile(f *ast.File, fileScope *Scope) {
	for _, d := range f.Decls {
		switch v := d.(type) {
		case *ast.TypeDecl:
			c.resolveTypeDecl(v, fileScope)
		case *ast.ClassDecl:
			c.resolveClassDecl(v, fileScope)
		case *ast.InterfaceDecl:
			c.resolveInterfaceDecl(v, fileScope)
		case *ast.FuncDecl:
			c.resolveFuncDecl(v, fileScope)
		case *ast.TopLevelLet:
			if v.DeclType != nil {
				c.resolveTypeExpr(v.DeclType, fileScope)
			}
			c.resolveExpr(v.Value, fileScope)
		case *ast.ExternFuncDecl:
			c.resolveExternFuncDecl(v, fileScope)
		case *ast.ExternImplDecl:
			c.resolveExternImplDecl(v, fileScope)
		}
	}
}

// resolveExternFuncDecl resolves a foreign function's signature
// annotations and freezes its Func type on the file-scope symbol
// (Barrier B). An extern func has no body (ffi.md §ExternFunc).
func (c *checker) resolveExternFuncDecl(fn *ast.ExternFuncDecl, parent *Scope) {
	c.checkReservedName(fn.Name, fn.Span)
	fnScope := newScope(parent)
	for _, tp := range fn.TypeParams {
		c.checkReservedName(tp.Name, fn.Span)
		c.checkTypeParamBound(tp, fn.Span)
		fnScope.declare(&Symbol{Name: tp.Name, Kind: SymTypeParam, Type: &Named{N: tp.Name}})
	}
	for _, p := range fn.Params {
		c.checkReservedName(p.Name, p.Span)
		c.resolveTypeExpr(p.DeclType, fnScope)
	}
	if fn.ReturnType != nil {
		c.resolveTypeExpr(fn.ReturnType, fnScope)
	}
	if sym := parent.lookup(fn.Name); sym != nil && sym.Kind == SymExternFunc {
		sym.Type = c.externFuncSigType(fn)
	}
}

// resolveExternImplDecl resolves the member signatures of an
// `extern impl T { … }` block. The named handle must be an
// `extern type` (E0103 otherwise); members carry no body.
func (c *checker) resolveExternImplDecl(ei *ast.ExternImplDecl, parent *Scope) {
	if sym := parent.lookup(ei.Type); sym == nil || sym.Kind != SymExternType {
		c.report("E0103", "Unknown extern type "+ei.Type, ei.Span)
	}
	for _, m := range ei.Methods {
		for _, p := range m.Params {
			c.checkReservedName(p.Name, p.Span)
			c.resolveTypeExpr(p.DeclType, parent)
		}
		if m.ReturnType != nil {
			c.resolveTypeExpr(m.ReturnType, parent)
		}
	}
	for _, f := range ei.Fields {
		c.checkReservedName(f.Name, f.Span)
		c.resolveTypeExpr(f.DeclType, parent)
	}
}

// resolveInterfaceDecl resolves an interface's `extends` list and the
// param / return types of its method signatures.
func (c *checker) resolveInterfaceDecl(id *ast.InterfaceDecl, parent *Scope) {
	scope := newScope(parent)
	for _, tp := range id.TypeParams {
		c.checkTypeParamBound(tp, id.Span)
		scope.declare(&Symbol{Name: tp.Name, Kind: SymTypeParam, Type: &Named{N: tp.Name}})
	}
	for _, e := range id.Extends {
		c.resolveTypeExpr(e, scope)
	}
	for _, m := range id.Methods {
		for _, prm := range m.Params {
			c.resolveTypeExpr(prm.DeclType, scope)
		}
		c.resolveTypeExpr(m.ReturnType, scope)
	}
}

func (c *checker) resolveTypeDecl(t *ast.TypeDecl, parent *Scope) {
	// Type parameters (`type Pair<T> = …`) are in scope inside the body
	// so a variant payload / record field / alias may reference them
	// (T-Generic-Decl). Mirrors resolveClassDecl.
	scope := parent
	if len(t.TypeParams) > 0 {
		scope = newScope(parent)
		for _, tp := range t.TypeParams {
			c.checkReservedName(tp.Name, t.Span)
			c.checkTypeParamBound(tp, t.Span)
			scope.declare(&Symbol{Name: tp.Name, Kind: SymTypeParam, Type: &Named{N: tp.Name}})
		}
	}
	switch b := t.Body.(type) {
	case *ast.AliasBody:
		c.resolveTypeExpr(b.Aliased, scope)
		c.checkComparableKeyBounds(t.TypeParams, []ast.TypeExpr{b.Aliased})
	case *ast.SumTypeBody:
		var fts []ast.TypeExpr
		for _, v := range b.Variants {
			for _, f := range v.Fields {
				c.resolveTypeExpr(f.DeclType, scope)
				fts = append(fts, f.DeclType)
			}
		}
		c.checkComparableKeyBounds(t.TypeParams, fts)
	case *ast.RecordTypeBody:
		fts := make([]ast.TypeExpr, 0, len(b.Fields))
		for _, f := range b.Fields {
			c.resolveTypeExpr(f.DeclType, scope)
			fts = append(fts, f.DeclType)
		}
		c.checkComparableKeyBounds(t.TypeParams, fts)
		// Bind a `contract <Record> { invariant … }`'s predicate fields in a
		// synthetic field scope (RFC-0006 type invariants on records).
		c.resolveRecordInvariants(t, b, scope)
	}
}

func (c *checker) resolveClassDecl(cd *ast.ClassDecl, parent *Scope) {
	classScope := newScope(parent)
	for _, tp := range cd.TypeParams {
		c.checkReservedName(tp.Name, cd.Span)
		c.checkTypeParamBound(tp, cd.Span)
		classScope.declare(&Symbol{Name: tp.Name, Kind: SymTypeParam, Type: &Named{N: tp.Name}})
	}
	// Resolve the `implements` list so an unknown interface name fires
	// E0103 and a known one is symbol-bound — the latter lets
	// satisfiesInterface follow a class → interface → `extends` chain.
	for _, impl := range cd.Implements {
		c.resolveTypeExpr(impl, parent)
	}
	// Resolve every field / method annotation against classScope
	// before building member symbols, so the signatures are fully
	// typed (Barrier B) regardless of declaration order.
	fieldTypes := make([]ast.TypeExpr, 0, len(cd.Fields))
	for _, f := range cd.Fields {
		c.resolveTypeExpr(f.DeclType, classScope)
		fieldTypes = append(fieldTypes, f.DeclType)
	}
	c.checkComparableKeyBounds(cd.TypeParams, fieldTypes)
	for _, m := range cd.Methods {
		for _, p := range m.Params {
			c.resolveTypeExpr(p.DeclType, classScope)
		}
		if m.ReturnType != nil {
			c.resolveTypeExpr(m.ReturnType, classScope)
		}
	}
	// Class member scope: fields + methods visible inside any
	// instance method body via implicit receiver
	// (name-resolution.md §Implicit receiver).
	memberScope := newScope(classScope)
	for _, f := range cd.Fields {
		c.checkReservedName(f.Name, f.Span)
		fsym := &Symbol{Name: f.Name, Kind: SymField, Decl: f, Type: c.typeFromExpr(f.DeclType)}
		memberScope.declare(fsym)
		c.info.Def[f] = fsym
	}
	for _, m := range cd.Methods {
		msym := &Symbol{Name: m.Name, Kind: SymMethod, Decl: m, Type: c.methodSigType(m)}
		memberScope.declare(msym)
		c.info.Def[m] = msym
	}
	for _, m := range cd.Methods {
		c.resolveMethod(cd, m, classScope, memberScope)
	}
	// Bind a `contract <Class> { invariant … }`'s predicates in member
	// scope (RFC-0006 type invariants) — after the members exist, so a
	// bare field name resolves to its SymField.
	c.resolveTypeInvariants(cd, memberScope)
}

func (c *checker) resolveMethod(cd *ast.ClassDecl, m *ast.Method, classScope, memberScope *Scope) {
	// Instance methods see members via implicit receiver; static
	// ones don't (they call other statics through the class name).
	var bodyParent *Scope
	if m.IsStatic {
		bodyParent = classScope
	} else {
		bodyParent = memberScope
	}
	bodyScope := newScope(bodyParent)
	if !m.IsStatic {
		bodyScope.declare(&Symbol{Name: "this", Kind: SymLocal, Decl: cd, Type: &Named{N: cd.Name, Decl: cd}})
	}
	for _, p := range m.Params {
		c.checkReservedName(p.Name, p.Span)
		c.resolveTypeExpr(p.DeclType, classScope)
		psym := &Symbol{Name: p.Name, Kind: SymLocal, Decl: p, Type: c.paramSymType(p)}
		bodyScope.declare(psym)
		c.info.Def[p] = psym
	}
	if m.ReturnType != nil {
		c.resolveTypeExpr(m.ReturnType, classScope)
	}
	if m.Body != nil {
		c.resolveBlock(m.Body, bodyScope)
	}
}

func (c *checker) resolveFuncDecl(fn *ast.FuncDecl, parent *Scope) {
	c.checkReservedName(fn.Name, fn.Span)
	fnScope := newScope(parent)
	for _, tp := range fn.TypeParams {
		c.checkReservedName(tp.Name, fn.Span)
		c.checkTypeParamBound(tp, fn.Span)
		fnScope.declare(&Symbol{Name: tp.Name, Kind: SymTypeParam, Type: &Named{N: tp.Name}})
	}
	for _, p := range fn.Params {
		c.checkReservedName(p.Name, p.Span)
		c.resolveTypeExpr(p.DeclType, fnScope)
		psym := &Symbol{Name: p.Name, Kind: SymLocal, Decl: p, Type: c.paramSymType(p)}
		fnScope.declare(psym)
		c.info.Def[p] = psym
	}
	if fn.ReturnType != nil {
		c.resolveTypeExpr(fn.ReturnType, fnScope)
	}
	// Freeze the function's external signature on its file-scope
	// symbol (Barrier B). The symbol lives in the parent scope.
	if sym := parent.lookup(fn.Name); sym != nil && sym.Kind == SymFunc {
		sym.Type = c.funcSigType(fn)
	}
	if fn.Body != nil {
		c.curContract = c.contractByTarget[fn.Name]
		c.matchedLoopLabels = map[string]bool{}
		c.resolveBlock(fn.Body, fnScope)
		c.reportUnmatchedLoopSections()
		c.resolveFuncContract(fn, fnScope)
		c.curContract = nil
	}
}

func (c *checker) resolveBlock(b *ast.Block, parent *Scope) {
	if b == nil {
		return
	}
	scope := newScope(parent)
	for _, s := range b.Stmts {
		c.resolveStmt(s, scope)
	}
	if b.Trailing != nil {
		c.resolveExpr(b.Trailing, scope)
	}
}

// recordUnusedCandidate remembers a `let`/`var` local so the deferred
// unused-local pass can flag it (E0221) if nothing ever referenced it.
// The check is deferred to a final pass (reportUnusedLocals) because a
// use may be recorded in a later phase — a loop `invariant` (resolution),
// a body reference (Barrier C), or a channel contract (last) — and the
// channel-type exemption needs the inferred type. The scope is captured
// so the pass can skip a binding shadowed by a same-scope re-declaration.
func (c *checker) recordUnusedCandidate(name string, sym *Symbol, span ast.Span, scope *Scope) {
	if sym == nil {
		return
	}
	c.unusedLocals = append(c.unusedLocals, unusedLocal{name: name, sym: sym, span: span, scope: scope})
}

// reportUnusedLocals is the deferred pass: after all phases have recorded
// every use, flag each `let`/`var` local that stayed unreferenced. Go
// rejects an unused local ("declared and not used") with a raw go/types
// message; we diagnose it in Aril terms (E0221, D10). Candidates are
// recorded in source order, so the diagnostics are deterministic.
func (c *checker) reportUnusedLocals() {
	for _, u := range c.unusedLocals {
		if u.sym.Used || u.sym.UsedInContract {
			continue
		}
		// Skip a binding shadowed by a same-scope re-declaration (the
		// scope's current occupant differs) — that mistake is already
		// E0222, so a second "unused" note would be redundant noise.
		if u.scope.names[u.name] != u.sym {
			continue
		}
		// A channel binding is exempt: channels participate in `select`,
		// `spawn`, and name-based channel contracts (RFC-0007) in ways the
		// Used flag does not capture, and an unused channel is a benign
		// resource, not a nil landmine. Only value bindings are checked.
		if isChannelType(u.sym.Type) {
			continue
		}
		c.report("E0221", "Unused local `"+u.name+"` — reference it, or bind `_` to discard", u.span)
	}
}

func (c *checker) resolveStmt(s ast.Stmt, scope *Scope) {
	switch v := s.(type) {
	case *ast.ExprStmt:
		c.resolveExpr(v.Expr, scope)
	case *ast.LetStmt:
		if v.Value != nil {
			c.resolveExpr(v.Value, scope)
		}
		if v.DeclType != nil {
			c.resolveTypeExpr(v.DeclType, scope)
		}
		c.bindPattern(v.Pattern, scope, v)
	case *ast.VarStmt:
		if v.Value != nil {
			c.resolveExpr(v.Value, scope)
		}
		if v.DeclType != nil {
			c.resolveTypeExpr(v.DeclType, scope)
		}
		if v.Name != "" && v.Name != "_" {
			c.checkReservedName(v.Name, v.Span)
			vsym := &Symbol{Name: v.Name, Kind: SymLocal, Decl: v, Type: &Unknown{}}
			if prev := scope.declare(vsym); prev != nil {
				c.report("E0222", "`"+v.Name+"` is already declared in this block — a nested `{ … }` block shadows instead", v.Span)
			}
			c.info.Def[v] = vsym
			c.recordUnusedCandidate(v.Name, vsym, v.Span, scope)
		}
	case *ast.AssignStmt:
		c.resolveExpr(v.LValue, scope)
		c.resolveExpr(v.Value, scope)
	case *ast.IfStmt:
		c.resolveExpr(v.Cond, scope)
		c.resolveBlock(v.ThenBlock, scope)
		switch e := v.Else.(type) {
		case *ast.IfStmt:
			c.resolveStmt(e, scope)
		case *ast.Block:
			c.resolveBlock(e, scope)
		}
	case *ast.WhileStmt:
		c.resolveExpr(v.Cond, scope)
		c.resolveLoopInvariants(v.Label, scope)
		c.resolveBlock(v.Body, scope)
	case *ast.DeferStmt:
		c.resolveExpr(v.Call, scope)
	case *ast.SelectStmt:
		for _, sc := range v.Cases {
			switch cse := sc.(type) {
			case *ast.SelectRecv:
				c.resolveExpr(cse.Channel, scope)
				caseScope := scope
				if cse.Bind != "" && cse.Bind != "_" {
					c.checkReservedName(cse.Bind, cse.Span)
					caseScope = newScope(scope)
					sym := &Symbol{Name: cse.Bind, Kind: SymLocal, Decl: cse, Type: &Unknown{}}
					caseScope.declare(sym)
					c.info.Def[cse] = sym
				}
				c.resolveBlock(cse.Body, caseScope)
			case *ast.SelectSend:
				c.resolveExpr(cse.Channel, scope)
				c.resolveExpr(cse.Value, scope)
				c.resolveBlock(cse.Body, scope)
			case *ast.SelectDefault:
				c.resolveBlock(cse.Body, scope)
			}
		}
	case *ast.ForStmt:
		// RangeExpr is a Node but not an Expr — handle it
		// explicitly. Other iterables (slices, maps, sets) are
		// regular Expr values.
		switch it := v.Iterable.(type) {
		case *ast.RangeExpr:
			c.resolveExpr(it.Low, scope)
			c.resolveExpr(it.High, scope)
		case ast.Expr:
			c.resolveExpr(it, scope)
		}
		bodyScope := newScope(scope)
		c.bindPattern(v.Pattern, bodyScope, v)
		// Loop invariants resolve in bodyScope — the loop variable and the
		// enclosing locals are visible; per-iteration body locals are not
		// (the invariant holds at the iteration boundary). RFC-0006.
		c.resolveLoopInvariants(v.Label, bodyScope)
		if v.Body != nil {
			// Re-use resolveBlock so block-internal scoping
			// stays consistent with the let-in-let rule.
			innerScope := newScope(bodyScope)
			for _, st := range v.Body.Stmts {
				c.resolveStmt(st, innerScope)
			}
			if v.Body.Trailing != nil {
				c.resolveExpr(v.Body.Trailing, innerScope)
			}
		}
	}
}

// bindPattern — introduces bindings from let/for/match patterns.
func (c *checker) bindPattern(p ast.Pattern, scope *Scope, decl any) {
	switch v := p.(type) {
	case *ast.IdentPat:
		if v.Name == "" || v.Name == "_" {
			return
		}
		c.checkReservedName(v.Name, v.Span)
		sym := &Symbol{Name: v.Name, Kind: SymLocal, Decl: decl, Type: &Unknown{}}
		if prev := scope.declare(sym); prev != nil {
			c.report("E0222", "`"+v.Name+"` is already declared in this block — a nested `{ … }` block shadows instead", v.Span)
		}
		c.info.Def[v] = sym
		// A `let` binding is checked for use; a `for`/`match`/`catch`
		// pattern binding is not (an ignored one gets the codegen `_ =`
		// guard, lowering-go.md §MatchIR), so record only let-introduced
		// locals for the unused-local pass.
		if _, isLet := decl.(*ast.LetStmt); isLet {
			c.recordUnusedCandidate(v.Name, sym, v.Span, scope)
		}
	case *ast.VariantPat:
		for _, sub := range v.Sub {
			c.bindPattern(sub, scope, decl)
		}
	case *ast.TuplePat:
		for _, sub := range v.Sub {
			c.bindPattern(sub, scope, decl)
		}
	case *ast.RecordPat:
		for _, f := range v.Fields {
			c.bindPattern(f.Pattern, scope, decl)
		}
	case *ast.AltPat:
		// Atoms are literal pats or nullary variants (P-Alt) — none
		// bind, but recurse so the empty-binding invariant holds even
		// if a future atom shape introduces one.
		for _, a := range v.Atoms {
			c.bindPattern(a, scope, decl)
		}
	}
}

func (c *checker) resolveTypeExpr(t ast.TypeExpr, scope *Scope) {
	if t == nil {
		return
	}
	switch v := t.(type) {
	case *ast.NamedType:
		if len(v.QName) > 0 {
			head := v.QName[0]
			if sym := scope.lookup(head); sym != nil {
				c.info.Symbol[v] = sym
			} else {
				c.report("E0103", "Unknown name "+head, v.Span)
			}
		}
		for _, a := range v.Args {
			c.resolveTypeExpr(a, scope)
		}
	case *ast.SliceType:
		c.resolveTypeExpr(v.Elem, scope)
	case *ast.TupleType:
		for _, ct := range v.Components {
			c.resolveTypeExpr(ct, scope)
		}
	case *ast.FuncType:
		for _, pt := range v.Params {
			c.resolveTypeExpr(pt, scope)
		}
		c.resolveTypeExpr(v.ReturnType, scope)
	}
}
