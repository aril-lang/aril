package sema

import (
	"strings"

	"github.com/aril-lang/aril/internal/ast"
)

// newPackageScope builds the package's top-level scope (parented by
// the predeclared scope). Every `.aril` file in the package shares it
// (RFC-0002 §"Package = directory").
func (c *checker) newPackageScope() *Scope {
	pre := newScope(nil)
	for name, sym := range predeclaredSymbols() {
		pre.names[name] = sym
	}
	return newScope(pre)
}

// indexFile registers one file's imports + top-level declarations into
// the (possibly shared) package scope — Barrier A, see
// docs/internals/sema.md §4. Duplicate top-level names — within a file
// or across files of the same package — are E0113.
func (c *checker) indexFile(f *ast.File, file *Scope) {
	for _, im := range f.Imports {
		if im.Path == "" {
			continue
		}
		head := strings.SplitN(im.Path, "/", 2)[0]
		// Only a predeclared (builtin) module name binds a namespace
		// symbol; the predeclared scope is the package scope's parent.
		if file.parent.lookup(head) == nil {
			continue
		}
		file.declare(&Symbol{Name: head, Kind: SymBuiltinModule, Type: &Unknown{}})
	}

	for _, d := range f.Decls {
		switch v := d.(type) {
		case *ast.TypeDecl:
			c.checkReservedName(v.Name, v.Span)
			c.checkReservedTypeName(v.Name, v.Span, file.parent)
			sym := &Symbol{Name: v.Name, Kind: SymTypeDecl, Decl: v, Type: &Named{N: v.Name, Decl: v}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
			if sb, ok := v.Body.(*ast.SumTypeBody); ok {
				// Within-sum duplicate variant names are E0106
				// per diagnostics.md.
				seen := map[string]bool{}
				for _, va := range sb.Variants {
					c.checkReservedName(va.Name, va.Span)
					if seen[va.Name] {
						c.report("E0106", "Duplicate variant name "+va.Name, va.Span)
						continue
					}
					seen[va.Name] = true
					vsym := &Symbol{Name: va.Name, Kind: SymUserVariant, Decl: va, Type: &Named{N: v.Name, Decl: v}}
					// Cross-sum ambiguity (E0104) — a variant
					// name shared by two different user sums.
					if prev := file.lookup(va.Name); prev != nil && prev.Kind == SymUserVariant {
						if prev.Type != nil && vsym.Type != nil && prev.Type.String() != vsym.Type.String() {
							c.report("E0104", "Ambiguous variant name "+va.Name+" — declared by both "+prev.Type.String()+" and "+vsym.Type.String(), va.Span)
						}
					}
					file.declare(vsym)
				}
			}
		case *ast.ClassDecl:
			c.checkReservedName(v.Name, v.Span)
			c.checkReservedTypeName(v.Name, v.Span, file.parent)
			sym := &Symbol{Name: v.Name, Kind: SymClass, Decl: v, Type: &Named{N: v.Name, Decl: v}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
		case *ast.InterfaceDecl:
			c.checkReservedName(v.Name, v.Span)
			c.checkReservedTypeName(v.Name, v.Span, file.parent)
			sym := &Symbol{Name: v.Name, Kind: SymInterface, Decl: v, Type: &Named{N: v.Name, Decl: v}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
		case *ast.FuncDecl:
			c.checkReservedName(v.Name, v.Span)
			sym := &Symbol{Name: v.Name, Kind: SymFunc, Decl: v, Type: &Unknown{}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
		case *ast.TopLevelLet:
			// Module-level constant. Type stays Unknown until the
			// body pass (checkTopLevelLet) infers it; keyed in
			// Info.Def[v] so that pass can write the resolved type
			// back onto this shared symbol.
			c.checkReservedName(v.Name, v.Span)
			sym := &Symbol{Name: v.Name, Kind: SymTopLevelLet, Decl: v, Type: &Unknown{}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
			c.info.Def[v] = sym
		case *ast.ExternTypeDecl:
			// Opaque foreign handle (ffi.md §ExternType). Its Type is
			// the nominal Named carrying the decl, so member access can
			// recognise the handle and refEq can admit it.
			c.checkReservedName(v.Name, v.Span)
			c.checkReservedTypeName(v.Name, v.Span, file.parent)
			sym := &Symbol{Name: v.Name, Kind: SymExternType, Decl: v, Type: &Named{N: v.Name, Decl: v}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
		case *ast.ExternFuncDecl:
			// Package-level foreign function. Signature frozen in the
			// resolve pass (Barrier B), like an ordinary FuncDecl.
			c.checkReservedName(v.Name, v.Span)
			sym := &Symbol{Name: v.Name, Kind: SymExternFunc, Decl: v, Type: &Unknown{}}
			if prev := file.declare(sym); prev != nil {
				c.report("E0113", "Duplicate top-level declaration "+v.Name, v.Span)
			}
		case *ast.ExternImplDecl:
			// Not a name binding — it attaches members to an existing
			// handle. Index by handle name for member-access lookup.
			c.externImpls[v.Type] = v
		}
	}
}

func (c *checker) checkReservedName(name string, span ast.Span) {
	if goReservedIdent(name) {
		c.report("E0107", "Reserved identifier prefix `_aril_` — used by codegen", span)
	}
}

// checkTypeParamBound validates a generic parameter's optional constraint
// bound: only the built-in constraints `Ordered` (→ `cmp.Ordered`) and
// `Comparable` (→ `comparable`) are admitted (E0119). An empty bound
// (unconstrained → `any`) is always fine. v1 has no user-defined constraints.
func (c *checker) checkTypeParamBound(tp ast.TypeParam, span ast.Span) {
	if tp.Bound == "" || tp.Bound == "Ordered" || tp.Bound == "Comparable" {
		return
	}
	c.report("E0119", "unknown type-parameter constraint `"+tp.Bound+"` — the built-in constraints are `Ordered` and `Comparable`", span)
}

// checkComparableKeyBounds reports E0127 when one of a declaration's own type
// parameters is used as a Map or Set key without a `Comparable`/`Ordered`
// bound. Go's map/set key type must satisfy `comparable`; an unconstrained
// (`any`) parameter in key position otherwise leaks a raw go/types "K does not
// satisfy comparable" at `go build` (a D10 violation). Aril requires the bound
// to be declared explicitly — D29: constraints mirror Go's and are stated,
// never inferred — and points the user at `K: Comparable`.
//
// Sound-over-complete (D38): only key positions reachable from the
// declaration's *field* types are scanned — the common case (`var index:
// Map<K, V>`). A Map/Set constructed on a bare parameter only inside a method
// body is out of v1 scope (it still fires the field-level check once the same
// parameter keys a field, as every real container does).
func (c *checker) checkComparableKeyBounds(params []ast.TypeParam, fields []ast.TypeExpr) {
	if len(params) == 0 {
		return
	}
	bound := make(map[string]string, len(params))
	for _, tp := range params {
		bound[tp.Name] = tp.Bound
	}
	reported := map[string]bool{}
	var scan func(te ast.TypeExpr)
	scan = func(te ast.TypeExpr) {
		switch t := te.(type) {
		case *ast.NamedType:
			if len(t.QName) == 1 && (t.QName[0] == "Map" || t.QName[0] == "Set") && len(t.Args) >= 1 {
				c.reportUnconstrainedKey(t.Args[0], bound, reported)
			}
			for _, a := range t.Args {
				scan(a)
			}
		case *ast.SliceType:
			scan(t.Elem)
		case *ast.TupleType:
			for _, comp := range t.Components {
				scan(comp)
			}
		case *ast.FuncType:
			for _, p := range t.Params {
				scan(p)
			}
			scan(t.ReturnType)
		}
	}
	for _, ft := range fields {
		scan(ft)
	}
}

// reportUnconstrainedKey fires E0127 for a Map/Set key that is a bare,
// unconstrained type parameter of the enclosing declaration. Each parameter is
// reported once (reported set). A non-parameter key (a concrete type, a nested
// generic) is Go's own business and is left alone.
func (c *checker) reportUnconstrainedKey(key ast.TypeExpr, bound map[string]string, reported map[string]bool) {
	nt, ok := key.(*ast.NamedType)
	if !ok || len(nt.QName) != 1 || len(nt.Args) != 0 {
		return
	}
	name := nt.QName[0]
	b, isParam := bound[name]
	if !isParam || b == "Comparable" || b == "Ordered" || reported[name] {
		return
	}
	reported[name] = true
	c.report("E0127", "type parameter `"+name+"` is used as a Map/Set key, so it must be comparable — add the bound `"+name+": Comparable`", nt.Span)
}

// checkReservedTypeName rejects a user type declaration that reuses a
// built-in type name — a primitive (`int`, `string`, …), `error`,
// `Any`/`Dynamic`/`unit`/`Never`, or a built-in generic (`Result`,
// `Option`, `Map`, …). Built-in type names are reserved: no
// `type`/`class`/`interface`/`extern type` may redeclare one (E0118).
// The set is sourced from the predeclared scope (file.parent), so it
// stays in lockstep with predeclaredSymbols. Value-level shadowing of a
// predeclared identifier (a local / parameter) is a separate, deferred
// concern (E0502/E0503). Contract: name-resolution.md §Reserved type
// names / keywords.md.
func (c *checker) checkReservedTypeName(name string, span ast.Span, pre *Scope) {
	if pre == nil {
		return
	}
	if sym := pre.lookup(name); sym != nil && sym.Kind == SymBuiltinType {
		c.report("E0118", "`"+name+"` is a built-in type and cannot be redeclared", span)
	}
}
