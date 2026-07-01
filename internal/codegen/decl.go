package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/binding"
)

// This file holds declaration-level emission: lowering Aril type,
// class, interface, method, and function declarations (and the
// type-expression printer they share) to Go. Split out of the
// codegen.go god-file; behaviour-preserving.

// emitTypeDecl lowers a TypeDecl. PR-F2 handles SumTypeBody
// (nullary-only) → Go `type T int` + `const (TVariant T = iota;
// ...)`, and AliasBody → Go `type T = U`.
func (g *gen) emitTypeDecl(td *ast.TypeDecl) error {
	switch body := td.Body.(type) {
	case *ast.AliasBody:
		g.line(td.Span.StartLine)
		g.b.WriteString("type ")
		g.b.WriteString(goIdent(td.Name))
		g.b.WriteString(" = ")
		if err := g.emitTypeExpr(body.Aliased); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	case *ast.RecordTypeBody:
		// A nominal record lowers to a named Go struct. Fields are
		// EXPORTED and carry a `json:"<arilName>"` tag so encoding/json
		// (reflecting from outside package main) round-trips them
		// (lowering-go.md §Record lowering).
		g.line(td.Span.StartLine)
		g.b.WriteString("type ")
		g.b.WriteString(goIdent(td.Name))
		g.emitTypeParamBrackets(td.TypeParams, true)
		g.b.WriteString(" struct {\n")
		g.indent++
		for _, f := range body.Fields {
			g.writeIndent()
			g.b.WriteString(exportFieldName(f.Name))
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(f.DeclType); err != nil {
				return err
			}
			g.writeJSONTag(f.Name)
			g.b.WriteByte('\n')
		}
		g.indent--
		g.b.WriteString("}\n")
		return nil
	case *ast.SumTypeBody:
		// Lower to a tagged struct per lowering-go.md §MatchIR.
		// The struct holds Tag + every variant's payload fields
		// (renamed to <VariantName><FieldName> to avoid clash
		// across variants). Nullary variants get a `var T_V =
		// T{Tag: N}`; payload variants get a `func T_V(...) T`
		// constructor. Tag is declaration order (§Variant-tag
		// numbering). A payload field that names the sum itself is
		// pointer-ized to break Go's infinite-size cycle (§Recursive
		// sum types).
		g.line(td.Span.StartLine)
		g.b.WriteString("type ")
		g.b.WriteString(goIdent(td.Name))
		g.emitTypeParamBrackets(td.TypeParams, true)
		g.b.WriteString(" struct {\n\tTag uint8\n")
		for _, v := range body.Variants {
			for _, f := range v.Fields {
				g.b.WriteByte('\t')
				g.b.WriteString(payloadFieldName(v.Name, f.Name))
				g.b.WriteByte(' ')
				if isSelfRefField(f, td.Name) {
					g.b.WriteByte('*')
				}
				if err := g.emitTypeExpr(f.DeclType); err != nil {
					return err
				}
				g.b.WriteByte('\n')
			}
		}
		g.b.WriteString("}\n")
		generic := len(td.TypeParams) > 0
		// Nullary variants. For a non-generic sum they are package-level
		// `var T_V = T{Tag: N}` consts; for a generic sum the value
		// needs a type argument Go can't supply at package scope, so each
		// becomes a parameterless generic constructor `func T_V[..]() T[..]`
		// (the OptionNone shape — §Generics).
		if generic {
			for i, v := range body.Variants {
				if len(v.Fields) != 0 {
					continue
				}
				g.b.WriteString("func ")
				g.b.WriteString(goIdent(td.Name))
				g.b.WriteString(goIdent(v.Name))
				g.emitTypeParamBrackets(td.TypeParams, true)
				g.b.WriteString("() ")
				g.b.WriteString(goIdent(td.Name))
				g.emitTypeParamBrackets(td.TypeParams, false)
				g.b.WriteString(" {\n\treturn ")
				g.b.WriteString(goIdent(td.Name))
				g.emitTypeParamBrackets(td.TypeParams, false)
				g.b.WriteString("{Tag: ")
				g.b.WriteString(strconv.Itoa(i))
				g.b.WriteString("}\n}\n")
			}
		} else {
			anyNullary := false
			for _, v := range body.Variants {
				if len(v.Fields) == 0 {
					anyNullary = true
					break
				}
			}
			if anyNullary {
				g.b.WriteString("var (\n")
				for i, v := range body.Variants {
					if len(v.Fields) != 0 {
						continue
					}
					g.b.WriteByte('\t')
					g.b.WriteString(goIdent(td.Name))
					g.b.WriteString(goIdent(v.Name))
					g.b.WriteString(" = ")
					g.b.WriteString(goIdent(td.Name))
					g.b.WriteByte('{')
					g.b.WriteString("Tag: ")
					g.b.WriteString(strconv.Itoa(i))
					g.b.WriteString("}\n")
				}
				g.b.WriteString(")\n")
			}
		}
		for i, v := range body.Variants {
			if len(v.Fields) == 0 {
				continue
			}
			g.b.WriteString("func ")
			g.b.WriteString(goIdent(td.Name))
			g.b.WriteString(goIdent(v.Name))
			g.emitTypeParamBrackets(td.TypeParams, true)
			g.b.WriteByte('(')
			for j, f := range v.Fields {
				if j > 0 {
					g.b.WriteString(", ")
				}
				g.b.WriteString(goIdent(f.Name))
				g.b.WriteByte(' ')
				if err := g.emitTypeExpr(f.DeclType); err != nil {
					return err
				}
			}
			g.b.WriteString(") ")
			g.b.WriteString(goIdent(td.Name))
			g.emitTypeParamBrackets(td.TypeParams, false)
			g.b.WriteString(" {\n\treturn ")
			g.b.WriteString(goIdent(td.Name))
			g.emitTypeParamBrackets(td.TypeParams, false)
			g.b.WriteByte('{')
			g.b.WriteString("Tag: ")
			g.b.WriteString(strconv.Itoa(i))
			for _, f := range v.Fields {
				g.b.WriteString(", ")
				g.b.WriteString(payloadFieldName(v.Name, f.Name))
				g.b.WriteString(": ")
				if isSelfRefField(f, td.Name) {
					g.b.WriteByte('&')
				}
				g.b.WriteString(goIdent(f.Name))
			}
			g.b.WriteString("}\n}\n")
		}
		return nil
	}
	return fmt.Errorf("codegen: unhandled TypeBody %T", td.Body)
}

// emitClassDecl lowers a ClassDecl per lowering-go.md
// §Implicit receiver. v1 always uses a pointer receiver for
// instance methods (§"For v1 every class uses a pointer
// receiver unconditionally"). Static methods lower to
// package-level functions named `<class-lowercase> + Cap(method)`.
func (g *gen) emitClassDecl(cd *ast.ClassDecl) error {
	g.line(cd.Span.StartLine)
	g.b.WriteString("type ")
	g.b.WriteString(goIdent(cd.Name))
	g.emitTypeParamBrackets(cd.TypeParams, true) // declaration: with `any` constraints
	g.b.WriteString(" struct {\n")
	for _, f := range cd.Fields {
		g.b.WriteByte('\t')
		g.b.WriteString(exportFieldName(f.Name))
		g.b.WriteByte(' ')
		if err := g.emitTypeExpr(f.DeclType); err != nil {
			return err
		}
		g.writeJSONTag(f.Name)
		g.b.WriteByte('\n')
	}
	g.b.WriteString("}\n")
	for _, m := range cd.Methods {
		if err := g.emitMethod(cd.Name, cd.TypeParams, m); err != nil {
			return err
		}
	}
	return nil
}

// emitInterfaceDecl lowers `interface Name { sig … }` to a Go
// interface. `extends` interfaces are embedded; method signatures
// emit `name(paramTypes) R`. Conformance is structural in Go, so a
// class that has the methods satisfies it (D14's explicit `implements`
// is sema-checked, not encoded in the Go interface).
func (g *gen) emitInterfaceDecl(id *ast.InterfaceDecl) error {
	g.line(id.Span.StartLine)
	g.b.WriteString("type ")
	g.b.WriteString(goIdent(id.Name))
	g.b.WriteString(" interface {\n")
	for _, e := range id.Extends {
		g.b.WriteByte('\t')
		if err := g.emitTypeExpr(e); err != nil {
			return err
		}
		g.b.WriteByte('\n')
	}
	for _, m := range id.Methods {
		g.b.WriteByte('\t')
		g.b.WriteString(goIdent(m.Name))
		g.b.WriteByte('(')
		for i, prm := range m.Params {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitTypeExpr(prm.DeclType); err != nil {
				return err
			}
		}
		g.b.WriteByte(')')
		if m.ReturnType != nil {
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(m.ReturnType); err != nil {
				return err
			}
		}
		g.b.WriteByte('\n')
	}
	g.b.WriteString("}\n")
	return nil
}

func (g *gen) emitMethod(className string, classTypeParams []ast.TypeParam, m *ast.Method) error {
	g.line(m.Span.StartLine)
	g.b.WriteString("func ")
	if !m.IsStatic {
		g.b.WriteString("(t *")
		g.b.WriteString(goIdent(className))
		g.emitTypeParamBrackets(classTypeParams, false) // receiver: type params without constraints
		g.b.WriteString(") ")
		g.b.WriteString(goIdent(m.Name))
	} else {
		g.b.WriteString(staticMethodName(className, m.Name))
		g.emitTypeParamBrackets(classTypeParams, true) // static = package-level func, declare constraints
	}
	g.b.WriteByte('(')
	for i, p := range m.Params {
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString(goIdent(p.Name))
		g.b.WriteByte(' ')
		if p.Variadic {
			g.b.WriteString("...")
		}
		if err := g.emitTypeExpr(p.DeclType); err != nil {
			return err
		}
	}
	g.b.WriteByte(')')
	if m.ReturnType != nil {
		g.b.WriteByte(' ')
		if err := g.emitTypeExpr(m.ReturnType); err != nil {
			return err
		}
	}
	g.b.WriteString(" {\n")
	g.indent++
	// Type invariants (RFC-0006) check at every method exit, on the
	// post-mutation receiver — emitted as a defer before the body. Static
	// methods have no receiver and are skipped (construction-time checking
	// is a later slice).
	if !m.IsStatic {
		if err := g.emitMethodInvariants(className); err != nil {
			return err
		}
	}
	prevRet := g.curFuncReturn
	g.curFuncReturn = m.ReturnType
	if err := g.emitFuncBody(m.Body, m.ReturnType, isUnitReturn(m.ReturnType)); err != nil {
		return err
	}
	g.curFuncReturn = prevRet
	g.indent--
	g.b.WriteString("}\n")
	return nil
}

// emitTypeParamBrackets writes a Go-side type-parameter
// list. With `withConstraints` it emits `[T any, U any, ...]`
// (used on declarations); without, it emits `[T, U, ...]`
// (used on uses like receiver types where the constraint has
// already been declared). An unconstrained parameter defaults to
// `any`; a built-in bound lowers to its Go constraint (G3b).
func (g *gen) emitTypeParamBrackets(tps []ast.TypeParam, withConstraints bool) {
	if len(tps) == 0 {
		return
	}
	g.b.WriteByte('[')
	for i, tp := range tps {
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString(goIdent(tp.Name))
		if withConstraints {
			g.b.WriteByte(' ')
			g.b.WriteString(goConstraint(tp.Bound))
		}
	}
	g.b.WriteByte(']')
}

// detectOrderedBound sets usesCmp if any generic declaration carries an
// `Ordered` type-parameter bound (which lowers to `cmp.Ordered`, needing
// `import "cmp"`). Run before writeHeader so the import is emitted.
func (g *gen) detectOrderedBound(f *ast.File) {
	hasOrdered := func(tps []ast.TypeParam) bool {
		for _, tp := range tps {
			if tp.Bound == "Ordered" {
				return true
			}
		}
		return false
	}
	// ExternFuncDecl is intentionally omitted: an extern lowers to a direct
	// `pkg.Sym(...)` call, never a Go generic signature carrying the
	// constraint, so it never needs `cmp` (sema still validates its bound).
	for _, d := range f.Decls {
		switch v := d.(type) {
		case *ast.FuncDecl:
			g.usesCmp = g.usesCmp || hasOrdered(v.TypeParams)
		case *ast.TypeDecl:
			g.usesCmp = g.usesCmp || hasOrdered(v.TypeParams)
		case *ast.ClassDecl:
			g.usesCmp = g.usesCmp || hasOrdered(v.TypeParams)
		case *ast.InterfaceDecl:
			g.usesCmp = g.usesCmp || hasOrdered(v.TypeParams)
		}
	}
}

// goConstraint maps an Aril type-parameter bound to its Go constraint. An
// empty bound (unconstrained) is Go `any`; the built-in bounds lower to
// `Ordered` → `cmp.Ordered` (needs `import "cmp"`, tracked by usesCmp) and
// `Comparable` → the Go built-in `comparable`. An unknown bound is rejected
// in sema (E0119) before reaching here.
func goConstraint(bound string) string {
	switch bound {
	case "Ordered":
		return "cmp.Ordered"
	case "Comparable":
		return "comparable"
	default:
		return "any"
	}
}

// staticMethodName returns the package-level Go name for a
// static method per lowering-go.md §Generics: `<className>` in
// camelCase + capitalised method name (`Counter.make` →
// `counterMake`).
func staticMethodName(className, methodName string) string {
	return lowerFirst(className) + capFirst(methodName)
}

// containerStaticCtorName returns the exported runtime constructor for a
// predeclared-container static call (`Map.new` → NewMap, `Set.from` →
// SetFrom, …), matching the arilrt package's exported names. Returns
// ("", false) for non-container static calls, which fall back to the
// user-class staticMethodName spelling. Block R: these names live in
// arilrt; the constructor name is the same in inline and vendored modes
// (the vendored caller prefixes it with the package selector).
func containerStaticCtorName(className, methodName string) (string, bool) {
	switch className {
	case "Map", "Set", "Stack":
		switch methodName {
		case "new":
			return "New" + className, true
		case "from":
			return className + capFirst(methodName), true
		}
	}
	return "", false
}

func (g *gen) emitFuncDecl(fn *ast.FuncDecl) error {
	g.line(fn.Span.StartLine)
	g.b.WriteString("func ")
	g.b.WriteString(goIdent(fn.Name))
	g.emitTypeParamBrackets(fn.TypeParams, true) // declaration: with `any` constraints
	g.b.WriteByte('(')
	for i, p := range fn.Params {
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString(goIdent(p.Name))
		g.b.WriteByte(' ')
		// A variadic `...T` parameter carries its element type in
		// DeclType; lower to Go's `...T` (ffi.md §Variadic).
		if p.Variadic {
			g.b.WriteString("...")
		}
		if err := g.emitTypeExpr(p.DeclType); err != nil {
			return err
		}
	}
	g.b.WriteByte(')')
	// A function whose contract has `ensures` lowers with a Go *named*
	// return value (`_arilRet`) so the deferred post-check sees the value at
	// every return path without rewriting returns (RFC-0006, panic mode).
	fc := g.contractFor(fn)
	named := fc != nil && len(fc.Ensures) > 0 && fn.ReturnType != nil
	if fn.ReturnType != nil {
		g.b.WriteByte(' ')
		if named {
			g.b.WriteString("(_arilRet ")
		}
		if err := g.emitTypeExpr(fn.ReturnType); err != nil {
			return err
		}
		if named {
			g.b.WriteByte(')')
		}
	}
	g.b.WriteString(" {\n")
	g.indent++
	if fc != nil {
		if err := g.emitContractPrologue(fc, fn, named); err != nil {
			return err
		}
	}
	prevRet := g.curFuncReturn
	g.curFuncReturn = fn.ReturnType
	if err := g.emitFuncBody(fn.Body, fn.ReturnType, isUnitReturn(fn.ReturnType)); err != nil {
		return err
	}
	g.curFuncReturn = prevRet
	g.indent--
	g.b.WriteString("}\n")
	return nil
}

// emitTypeExpr lowers a TypeExpr to its Go form. PR-F1 handles
// PrimitiveType and NamedType; SliceType / TupleType / FuncType /
// InlineInterface land with later PRs.
func (g *gen) emitTypeExpr(t ast.TypeExpr) error {
	switch v := t.(type) {
	case *ast.PrimitiveType:
		// Aril primitive names map 1:1 onto Go's by spec
		// (lowering-go.md §Primitive type lowering); the only
		// transform is `unit` → Go's zero-byte `struct{}`.
		if v.Name == "unit" {
			g.b.WriteString("struct{}")
			return nil
		}
		g.b.WriteString(v.Name)
		return nil
	case *ast.SliceType:
		g.b.WriteString("[]")
		return g.emitTypeExpr(v.Elem)
	case *ast.TupleType:
		// Tuples lower to anonymous Go structs with positional
		// fields `_0`, `_1`, … — structurally typed, so equal-shape
		// tuples share a Go type without a named declaration
		// (lowering-go.md §Tuple lowering).
		g.b.WriteString("struct { ")
		for i, ct := range v.Components {
			if i > 0 {
				g.b.WriteString("; ")
			}
			g.b.WriteString("_")
			g.b.WriteString(strconv.Itoa(i))
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(ct); err != nil {
				return err
			}
		}
		g.b.WriteString(" }")
		return nil
	case *ast.FuncType:
		// `func(A, B): R` → Go `func(A, B) R`.
		g.b.WriteString("func(")
		for i, pt := range v.Params {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitTypeExpr(pt); err != nil {
				return err
			}
		}
		g.b.WriteByte(')')
		if v.ReturnType != nil {
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(v.ReturnType); err != nil {
				return err
			}
		}
		return nil
	case *ast.NamedType:
		// Channel types lower to Go's native channel types
		// (lowering-go.md §Channel lowering): Channel<T> → `chan T`,
		// SendChan<T> → `chan<- T`, RecvChan<T> → `<-chan T`. Not a
		// wrapper struct — channels are a first-class Go primitive.
		if len(v.QName) == 1 && len(v.Args) == 1 {
			var prefix string
			switch v.QName[0] {
			case "Channel":
				prefix = "chan "
			case "SendChan":
				prefix = "chan<- "
			case "RecvChan":
				prefix = "<-chan "
			}
			if prefix != "" {
				g.b.WriteString(prefix)
				return g.emitTypeExpr(v.Args[0])
			}
		}
		// An opaque foreign handle (ffi.md §ExternType) lowers to the
		// Go pointer type `*pkg.Sym` — the `*regexp.Regexp` / `*exec.Cmd`
		// shape Go libraries are used through.
		if len(v.QName) == 1 {
			if etd, isHandle := g.externType[v.QName[0]]; isHandle {
				pkg, sym := goRefPkgSym(etd.Go, etd.Name)
				g.b.WriteByte('*')
				if pkg != "" {
					g.b.WriteString(goPkgRef(pkg))
					g.b.WriteByte('.')
				}
				g.b.WriteString(sym)
				return nil
			}
		}
		// Per G16 / lowering-go.md §Implicit receiver, classes
		// are reference types — a NamedType naming a class in
		// scope lowers to `*ClassName` in Go so that field
		// mutation through methods is visible to all aliases.
		if len(v.QName) == 1 {
			if _, isClass := g.class[v.QName[0]]; isClass {
				g.b.WriteByte('*')
			}
		}
		// A bound value-handle type (regexp.Regexp → *regexp.Regexp, …) lowers
		// to its Go type spelling, which may differ from the Aril spelling in
		// both pointer-ness and package (VALUE-HANDLES). The package's import is
		// kept by the pre-walk (markHandlePkgs), which runs before writeHeader.
		if len(v.QName) > 1 {
			if ht, ok := binding.HandleTypeOf(strings.Join(v.QName, ".")); ok {
				g.b.WriteString(ht.GoType)
				return nil
			}
		}
		// Predeclared runtime types take the arilrt package selector in
		// vendored mode; everything else (user types, qualified names, a
		// user type shadowing a runtime name) keeps its plain spelling.
		if len(v.QName) == 1 && isRuntimeTypeName(v.QName[0]) && !g.isShadowedRuntimeType(v.QName[0]) {
			g.b.WriteString(g.rt(v.QName[0]))
		} else {
			g.b.WriteString(strings.Join(v.QName, "."))
		}
		if err := g.emitTypeArgs(v.Args); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("codegen: unhandled type expression %T", t)
}
