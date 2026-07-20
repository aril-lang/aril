package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/binding"
	"github.com/aril-lang/aril/internal/sema"
)

// This file holds expression-level emission: the brace/tuple/reflect
// literal emitters, the core emitExpr / emitField walk, and the
// field/method-name resolution helpers they rely on. Split out of
// the codegen.go god-file; behaviour-preserving.

// containerCtorName returns the arilrt empty-constructor name for a
// builtin reference-container type expression (List/Map/Set/Stack) and
// true, or "" and false for any other type. The empty container is the
// safe zero value of a reference container — never a Go nil
// (lowering-go.md §Container defaulting; T2/T13/bug#3).
func containerCtorName(t ast.TypeExpr) (string, bool) {
	nt, ok := t.(*ast.NamedType)
	if !ok || len(nt.QName) != 1 {
		return "", false
	}
	switch nt.QName[0] {
	case "List":
		return "NewList", true
	case "Map":
		return "NewMap", true
	case "Set":
		return "NewSet", true
	case "Stack":
		return "NewStack", true
	}
	return "", false
}

// emitEmptyContainer writes the empty constructor for a container type
// expression — `List<int>` → `NewList[int]()`. It reports whether t was
// a container (and thus whether anything was written), so callers can
// fall back to a different default for non-container types.
func (g *gen) emitEmptyContainer(t ast.TypeExpr) (bool, error) {
	ctor, ok := containerCtorName(t)
	if !ok {
		return false, nil
	}
	g.b.WriteString(g.rt(ctor))
	if err := g.emitTypeArgs(t.(*ast.NamedType).Args); err != nil {
		return false, err
	}
	g.b.WriteString("()")
	return true, nil
}

// emitBraceLit lowers a brace literal. A record literal becomes a Go
// struct literal `TypeName{ field: value, … }` (same-package field
// names map directly). Map / Set / Stack literals lower to the
// predeclared container helpers, sharing the `.new()` / `.from()`
// representation (builtins.md §Map / §Set / §Stack).
func (g *gen) emitBraceLit(b *ast.BraceLit) error {
	if len(b.TypeName.QName) == 1 {
		switch b.TypeName.QName[0] {
		case "Map":
			return g.emitMapBraceLit(b)
		case "Set":
			return g.emitSetBraceLit(b)
		case "Stack":
			return g.emitStackBraceLit(b)
		case "List":
			return g.emitListBraceLit(b)
		}
	}
	// An empty `T{}` parses as BraceUnknown (no entries to commit a kind). The
	// Map/Set/Stack names were dispatched above, so a remaining empty literal on
	// a named type is an empty struct literal — a fieldless class is a valid
	// behaviour-only interface implementor (the strategy/visitor pattern). It
	// takes the record/class path, emitting `&T{}` (class) or `T{}` (record).
	emptyStruct := b.Kind == ast.BraceUnknown && len(b.Entries) == 0
	if b.Kind != ast.BraceRecord && !emptyStruct {
		return fmt.Errorf("codegen: %s brace literal not yet supported — use the container constructor / `.new()`", b.Kind)
	}
	if len(b.TypeName.QName) != 1 {
		// A qualified name here is a constructable stdlib handle, empty-only
		// (lowering-go §Brace literals).
		spelled := strings.Join(b.TypeName.QName, ".")
		// atomic.Pointer<T>{} — the generic atomic cell (ATOMICS-BINDING). A fresh
		// heap cell `&arilrt.AtomicPointer[P]{}` (a pointer, like a class, so it is
		// shared by reference and never copied). sema rejects a non-empty literal
		// (E0218), so any entry is ignored here.
		if spelled == "atomic.Pointer" && len(b.TypeName.Args) == 1 {
			g.b.WriteString("&" + g.rt("AtomicPointer") + "[")
			if err := g.emitPointeeType(b.TypeName.Args[0]); err != nil {
				return err
			}
			g.b.WriteString("]{}")
			return nil
		}
		if ht, ok := binding.HandleTypeOf(spelled); ok && ht.Constructable {
			initFields, hasInit := binding.HandleInitFieldsOf(spelled)
			if len(b.Entries) != 0 && !hasInit {
				return fmt.Errorf("codegen: %s takes no fields — construct it as %s{}", spelled, spelled)
			}
			if ht.GoPkg != "" {
				g.usedGoPkgs[ht.GoPkg] = true
			}
			// A pointer handle (`*http.Server`) lowers to `&http.Server{…}`; a value
			// handle (`sync.Mutex`) to `sync.Mutex{…}`. Init fields (sema-validated)
			// map Aril name → exported Go field, in source order.
			goType := ht.GoType
			if strings.HasPrefix(goType, "*") {
				g.b.WriteByte('&')
				goType = goType[1:]
			}
			g.b.WriteString(goType)
			g.b.WriteByte('{')
			for i, e := range b.Entries {
				re, ok := e.(*ast.RecordEntry)
				if !ok {
					return fmt.Errorf("codegen: %s is constructed with named fields", spelled)
				}
				fb, ok := initFields[re.Name]
				if !ok {
					return fmt.Errorf("codegen: %s has no constructable field %s", spelled, re.Name)
				}
				if i > 0 {
					g.b.WriteString(", ")
				}
				g.b.WriteString(fb.GoName)
				g.b.WriteString(": ")
				if err := g.emitExpr(re.Value); err != nil {
					return err
				}
			}
			g.b.WriteByte('}')
			return nil
		}
		return fmt.Errorf("codegen: qualified record type name not supported")
	}
	name := b.TypeName.QName[0]
	ci, isClass := g.class[name]
	if isClass && ci.generic && len(b.TypeName.Args) == 0 {
		return fmt.Errorf("codegen: brace literal on generic class %s needs explicit type arguments — write %s<T>{…}", name, name)
	}
	// A type carrying `invariant`s (RFC-0006) is checked at construction:
	// the literal lowers inside a `func() T { _arilNew := <lit>; <checks>;
	// return _arilNew }()` so a brace literal in any expression position is
	// validated before use. Construction is the only checkpoint for a record
	// (no methods); for a class it complements the method-exit checks. Under
	// off / no-invariant the literal lowers bare (byte-identical).
	preds := g.constructionInvariants(name)
	if len(preds) > 0 {
		g.b.WriteString("func() ")
		if isClass {
			g.b.WriteByte('*')
		}
		g.b.WriteString(goIdent(name))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString(" {\n")
		g.indent++
		g.writeIndent()
		g.b.WriteString("_arilNew := ")
	}
	// A class is a reference type — `Bar{ x: 6 }` constructs `&Bar{…}`
	// so its methods (declared on `*Bar`) are reachable.
	if isClass {
		g.b.WriteByte('&')
	}
	g.b.WriteString(goIdent(name))
	// Generic record/class literal `Box<int>{…}` lowers to the
	// instantiated Go type `Box[int]{…}` — Go cannot infer struct
	// type parameters from a composite literal.
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteByte('{')
	for i, e := range b.Entries {
		re, ok := e.(*ast.RecordEntry)
		if !ok {
			return fmt.Errorf("codegen: non-record entry %T in record literal", e)
		}
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString(exportFieldName(re.Name))
		g.b.WriteString(": ")
		// Flow the field's declared type as the expected type so a
		// constructor value Go can't infer — `q: None` /
		// `r: Ok(v)` — gets its type args stamped (§Constructor
		// type-argument stamping). nil when the field type is unknown.
		prevExpect := g.expectType
		if fts, ok := g.fieldTypes[name]; ok {
			g.expectType = fts[re.Name]
		} else {
			g.expectType = nil
		}
		err := g.emitExpr(re.Value)
		g.expectType = prevExpect
		if err != nil {
			return err
		}
	}
	// Fill omitted reference-container fields with the empty constructor.
	// Go's zero value for a `*arilrt.List[T]` field is a nil pointer, which
	// segfaults on first use (T2); the empty container is the sound default
	// and honours the "no nil" promise (lowering-go.md §Container defaulting).
	if err := g.fillOmittedContainerFields(name, b.TypeName.Args, b.Entries, len(b.Entries) > 0); err != nil {
		return err
	}
	g.b.WriteByte('}')
	if len(preds) > 0 {
		g.b.WriteByte('\n')
		if err := g.emitConstructionInvariants(name, preds); err != nil {
			return err
		}
		g.writeIndent()
		g.b.WriteString("return _arilNew\n")
		g.indent--
		g.writeIndent()
		g.b.WriteString("}()")
	}
	return nil
}

// fillOmittedContainerFields appends `Field: NewList[T]()` entries for
// every declared reference-container field of type `name` that the
// literal omitted, so the field defaults to an empty container rather
// than a Go nil pointer (T2). `args` are the construction's concrete
// type arguments (`Box<int>{…}` → [int]); a `List<T>` field is
// substituted to `List<int>` before emit so the fill never references
// the enclosing type's parameter `T`, which is out of scope at the
// construction site (would leak a raw Go `undefined: T`). `wrote` is
// whether the caller already emitted at least one keyed entry (governs
// the leading comma). Fields are walked in declaration order for
// deterministic output; non-container omitted fields keep Go's zero
// value, safe for scalars/Option/records (lowering-go.md §Container
// defaulting).
func (g *gen) fillOmittedContainerFields(name string, args []ast.TypeExpr, entries []ast.BraceEntry, wrote bool) error {
	fts, ok := g.fieldTypes[name]
	if !ok {
		return nil
	}
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		if re, ok := e.(*ast.RecordEntry); ok {
			present[re.Name] = true
		}
	}
	// Map the type's declared parameters to the construction's concrete
	// args (empty for a non-generic type — substitution is then a no-op).
	subst := map[string]ast.TypeExpr{}
	if params := g.typeParams[name]; len(params) == len(args) {
		for i, p := range params {
			subst[p] = args[i]
		}
	}
	for _, fn := range g.fieldOrder[name] {
		if present[fn] {
			continue
		}
		if _, isContainer := containerCtorName(fts[fn]); !isContainer {
			continue
		}
		if wrote {
			g.b.WriteString(", ")
		}
		g.b.WriteString(exportFieldName(fn))
		g.b.WriteString(": ")
		if _, err := g.emitEmptyContainer(substTypeParams(fts[fn], subst)); err != nil {
			return err
		}
		wrote = true
	}
	return nil
}

// substTypeParams returns t with every bare type-parameter reference
// replaced per subst (a `NamedType` of one segment, no args, whose name
// is a key). Container/generic type arguments are rewritten recursively
// (`List<T>` → `List<int>`, `Map<K,V>` → `Map<string,int>`). A no-op
// when subst is empty or t names no parameter. Only the NamedType shape
// carries parameters in a container field type, so other TypeExpr kinds
// pass through unchanged.
func substTypeParams(t ast.TypeExpr, subst map[string]ast.TypeExpr) ast.TypeExpr {
	if len(subst) == 0 {
		return t
	}
	nt, ok := t.(*ast.NamedType)
	if !ok {
		return t
	}
	if len(nt.QName) == 1 && len(nt.Args) == 0 {
		if repl, ok := subst[nt.QName[0]]; ok {
			return repl
		}
		return t
	}
	if len(nt.Args) == 0 {
		return t
	}
	newArgs := make([]ast.TypeExpr, len(nt.Args))
	changed := false
	for i, a := range nt.Args {
		newArgs[i] = substTypeParams(a, subst)
		if newArgs[i] != a {
			changed = true
		}
	}
	if !changed {
		return t
	}
	return &ast.NamedType{Span: nt.Span, QName: nt.QName, Args: newArgs}
}

// emitSetBraceLit lowers `Set<T>{}` → `NewSet[T]()` and
// `Set<T>{e1,…}` → `SetFrom([]T{e1,…})`, reusing the predeclared Set
// helpers (Go infers `SetFrom`'s `T` from the slice literal).
func (g *gen) emitSetBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) == 0 {
		g.b.WriteString(g.rt("NewSet"))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString("()")
		return nil
	}
	if len(b.TypeName.Args) != 1 {
		return fmt.Errorf("codegen: Set literal needs an element type argument — write Set<T>{…}")
	}
	g.b.WriteString(g.rt("SetFrom") + "([]")
	if err := g.emitTypeExpr(b.TypeName.Args[0]); err != nil {
		return err
	}
	g.b.WriteByte('{')
	for i, e := range b.Entries {
		se, ok := e.(*ast.SetEntry)
		if !ok {
			return fmt.Errorf("codegen: non-set entry %T in Set literal", e)
		}
		if i > 0 {
			g.b.WriteString(", ")
		}
		if err := g.emitExpr(se.Value); err != nil {
			return err
		}
	}
	g.b.WriteString("})")
	return nil
}

// emitMapBraceLit lowers `Map<K,V>{}` → `NewMap[K,V]()` and a
// non-empty `Map<K,V>{ k: v, … }` to an insertion IIFE
// (`func() *Map[K,V] { m := NewMap[K,V](); m.set(k, v); …; return m }()`)
// — Map has no construct-from-entries helper, and an IIFE keeps the
// literal a single Go expression.
func (g *gen) emitMapBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) == 0 {
		g.b.WriteString(g.rt("NewMap"))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString("()")
		return nil
	}
	if len(b.TypeName.Args) != 2 {
		return fmt.Errorf("codegen: Map literal needs key and value type arguments — write Map<K,V>{…}")
	}
	g.b.WriteString("func() *Map")
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString(" { m := NewMap")
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString("(); ")
	for _, e := range b.Entries {
		me, ok := e.(*ast.MapEntry)
		if !ok {
			return fmt.Errorf("codegen: non-map entry %T in Map literal", e)
		}
		g.b.WriteString("m.Set(")
		if err := g.emitExpr(me.Key); err != nil {
			return err
		}
		g.b.WriteString(", ")
		if err := g.emitExpr(me.Value); err != nil {
			return err
		}
		g.b.WriteString("); ")
	}
	g.b.WriteString("return m }()")
	return nil
}

// emitStackBraceLit lowers `Stack<T>{}` → `NewStack[T]()`. A Stack
// literal is always empty (ast.md §BraceLit); sema rejects entries.
func (g *gen) emitStackBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) != 0 {
		return fmt.Errorf("codegen: Stack literal must be empty — push elements after construction")
	}
	g.b.WriteString(g.rt("NewStack"))
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString("()")
	return nil
}

// emitListBraceLit lowers `List<T>{}` → `NewList[T]()` and the initialized
// `List<T>{a, b, c}` → `ListOf[T](a, b, c)` (builtins.md §List). Entries are
// bare values (parsed as SetEntry).
func (g *gen) emitListBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) == 0 {
		g.b.WriteString(g.rt("NewList"))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString("()")
		return nil
	}
	g.b.WriteString(g.rt("ListOf"))
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteByte('(')
	for i, e := range b.Entries {
		if i > 0 {
			g.b.WriteString(", ")
		}
		se, ok := e.(*ast.SetEntry)
		if !ok {
			return fmt.Errorf("codegen: non-value entry %T in List literal", e)
		}
		if err := g.emitExpr(se.Value); err != nil {
			return err
		}
	}
	g.b.WriteByte(')')
	return nil
}

// emitTupleLit lowers a tuple literal to an anonymous-struct literal.
// The struct type comes from sema's inferred Tuple so the literal
// shares its Go type with any matching annotation / field access
// (structural equivalence).
func (g *gen) emitTupleLit(t *ast.TupleLit) error {
	var structType string
	if g.info != nil {
		if tt, ok := g.info.Type[t].(*sema.Tuple); ok {
			if s, ok := g.goTypeFromSema(tt); ok {
				structType = s
			}
		}
	}
	if structType == "" {
		return fmt.Errorf("codegen: cannot infer Go type for tuple literal — annotate the binding")
	}
	g.b.WriteString(structType)
	g.b.WriteByte('{')
	for i, ce := range t.Components {
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString("_")
		g.b.WriteString(strconv.Itoa(i))
		g.b.WriteString(": ")
		if err := g.emitExpr(ce); err != nil {
			return err
		}
	}
	g.b.WriteByte('}')
	return nil
}

// emitReflectCall lowers a `reflect.X(args)` call to the
// corresponding inline arilrt helper emitted by
// `writePredeclaredReflect`. Current surface: box / unbox /
// typeOf / typeName / kind / fields / fieldValue / show
// (PR-R1 .. PR-R3). Variants / methods / typeArgs / elementType
// land with later Block-R PRs.
// reflectFuncName maps a reflect.* method name to its arilrt export.
// kind → KindOf avoids clashing with the Kind type.
var reflectFuncName = map[string]string{
	"typeOf":     "TypeOf",
	"typeName":   "TypeName",
	"kind":       "KindOf",
	"fields":     "Fields",
	"fieldValue": "FieldValue",
	"show":       "Show",
}

func (g *gen) emitReflectCall(name string, typeArgs []ast.TypeExpr, args []ast.Expr) error {
	switch name {
	case "box":
		g.b.WriteString(g.rt("Box"))
		if len(typeArgs) > 0 {
			g.b.WriteByte('[')
			if err := g.emitTypeExpr(typeArgs[0]); err != nil {
				return err
			}
			g.b.WriteByte(']')
		}
		g.b.WriteByte('(')
		if len(args) != 1 {
			return fmt.Errorf("codegen: reflect.box expects exactly one argument, got %d", len(args))
		}
		if err := g.emitExpr(args[0]); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case "unbox":
		if len(typeArgs) != 1 {
			return fmt.Errorf("codegen: reflect.unbox requires exactly one explicit type argument `reflect.unbox<T>(d)`")
		}
		g.b.WriteString(g.rt("Unbox") + "[")
		if err := g.emitTypeExpr(typeArgs[0]); err != nil {
			return err
		}
		g.b.WriteString("](")
		if len(args) != 1 {
			return fmt.Errorf("codegen: reflect.unbox expects exactly one argument, got %d", len(args))
		}
		if err := g.emitExpr(args[0]); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case "typeOf", "typeName", "kind", "fields", "fieldValue", "show":
		g.b.WriteString(g.rt(reflectFuncName[name]))
		g.b.WriteByte('(')
		for i, a := range args {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitExpr(a); err != nil {
				return err
			}
		}
		g.b.WriteByte(')')
		return nil
	}
	return fmt.Errorf("codegen: reflect.%s is not yet supported (methods / variants / variantOf / typeArgs / elementType land later)", name)
}

// semaSliceElem returns the Go element type for an inferred slice
// literal from sema's side-table — used when literal-only inference
// (inferSliceElemType) can't see the element type (e.g. `[v]` with an
// Ident / call element). Returns ("", false) when no usable sema type
// is available, so the caller falls back to literal inference.
func (g *gen) semaSliceElem(lit *ast.SliceLit) (string, bool) {
	if g.info == nil {
		return "", false
	}
	st, ok := g.info.Type[lit].(*sema.Slice)
	if !ok {
		return "", false
	}
	return g.goTypeFromSema(st.Elem)
}

// inferSliceElemType returns the Go-side element type for an
// inferred slice literal. PR-F3 supports Int / String / Bool
// literal elements; anything else returns an error.
func inferSliceElemType(items []ast.Expr) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("codegen: empty inferred-type slice literal — annotate with `[]T{}`")
	}
	switch items[0].(type) {
	case *ast.IntLitExpr:
		return "int", nil
	case *ast.FloatLitExpr:
		return "float64", nil
	case *ast.StringLitExpr:
		return "string", nil
	case *ast.BoolLitExpr:
		return "bool", nil
	}
	return "", fmt.Errorf("codegen: cannot infer element type from %T — annotate the slice literal", items[0])
}

// ---- expressions ----

func (g *gen) emitExpr(e ast.Expr) error {
	switch v := e.(type) {
	case *ast.IntLitExpr:
		g.b.WriteString(strconv.FormatInt(v.Value, 10))
		return nil
	case *ast.FloatLitExpr:
		// Re-emit source text; Go accepts the same `3.14` / `1e3`
		// float syntax for its float64.
		g.b.WriteString(v.RawText)
		return nil
	case *ast.StringLitExpr:
		g.b.WriteString(strconv.Quote(v.Value))
		return nil
	case *ast.StringInterpExpr:
		return g.emitStringInterp(v)
	case *ast.RuneLitExpr:
		// Re-emit the source text; Go accepts the same `'a'`
		// rune-literal syntax for its rune (int32) type.
		g.b.WriteString(v.RawText)
		return nil
	case *ast.BoolLitExpr:
		if v.Value {
			g.b.WriteString("true")
		} else {
			g.b.WriteString("false")
		}
		return nil
	case *ast.UnitLit:
		// The unit value `()` is Go's zero-byte composite literal
		// (lowering-go.md §Primitive type lowering).
		g.b.WriteString("struct{}{}")
		return nil
	case *ast.ScopeRef:
		// A bare `scope` value lowers to its context (the only v1 handle
		// operation); `scope.context` is intercepted in emitField.
		return g.emitScopeContext(v.Span)
	case *ast.ScopeExpr:
		// Value-position structured-concurrency scope → IIFE
		// returning Result[T, error] (lowering-go.md §ScopeIR).
		return g.emitScopeExpr(v)
	case *ast.ThisExpr:
		// lowering-go.md §Implicit receiver — the receiver carries a
		// reserved name (methodRecvName) so it can't collide with a user
		// binding of the same spelling.
		g.b.WriteString(methodRecvName)
		return nil
	case *ast.Ident:
		// Inside an emitted contract predicate (RFC-0006): `result` is the
		// function's return value (the Go named return `_arilRet`), and an
		// entry-binding name resolves to its entry temp. Set while emitting
		// requires/ensures; empty elsewhere.
		if g.contractResultVar != "" && v.Name == "result" {
			g.b.WriteString(g.contractResultVar)
			return nil
		}
		if ev, ok := g.contractEntryVars[v.Name]; ok {
			g.b.WriteString(ev)
			return nil
		}
		// Variant identifiers (declared in any sum type in the
		// same file) get qualified to their Go-side variable:
		// `Red` → `ColorRed`.
		if info, ok := g.variant[v.Name]; ok {
			// A bare nullary constructor of a *generic* sum is a
			// parameterless generic call Go can't infer — stamp explicit
			// type args (`OptionNone[T]()`, `TreeLeaf[int]()`;
			// lowering-go.md §Container types / §Generics). The args come
			// from the enclosing ctor call's inferred instantiation
			// (g.sumCtorArgs, set while emitting a `Node(…)`'s arguments),
			// else the expected type. User sums with no type params and
			// all other variants emit bare.
			if len(info.sumTypeParams) > 0 {
				if len(g.sumCtorArgs) == len(info.sumTypeParams) {
					g.b.WriteString(g.sumOwnerName(info.owner))
					g.b.WriteString(goIdent(v.Name))
					g.b.WriteByte('[')
					g.b.WriteString(strings.Join(g.sumCtorArgs, ", "))
					g.b.WriteString("]()")
					return nil
				}
				if targs, ok := g.userSumCtorArgsFromExpect(info, g.expectType); ok {
					g.b.WriteString(g.sumOwnerName(info.owner))
					g.b.WriteString(goIdent(v.Name))
					if err := g.emitTypeArgs(targs); err != nil {
						return err
					}
					g.b.WriteString("()")
					return nil
				}
			}
			if targs, _, ok := g.predeclaredCtorTypeArgs(v.Name, g.expectType); ok {
				g.b.WriteString(g.sumOwnerName(info.owner))
				g.b.WriteString(goIdent(v.Name))
				if err := g.emitTypeArgs(targs); err != nil {
					return err
				}
				g.b.WriteString("()")
				return nil
			}
			g.b.WriteString(g.sumOwnerName(info.owner))
			g.b.WriteString(goIdent(v.Name))
			return nil
		}
		// A bare ident that sema resolved to a class/record field (not a
		// shadowing local/param) is an implicit-receiver reference
		// (name-resolution §Implicit receiver) — emit `<recv>.<field>`,
		// since the Go field lives on the method receiver (methodRecvName)
		// or, inside a type-invariant construction check, the construction temp.
		if g.info != nil {
			if sym := g.info.Symbol[v]; sym != nil && sym.Kind == sema.SymField {
				recv := g.contractReceiver
				if recv == "" {
					recv = methodRecvName
				}
				g.b.WriteString(recv)
				g.b.WriteByte('.')
				g.b.WriteString(exportFieldName(v.Name))
				return nil
			}
		}
		g.b.WriteString(goIdent(v.Name))
		return nil
	case *ast.SliceLit:
		// Annotated form `[]T{...}` → `[]T{...}` directly.
		// Inferred form `[e_1, ..., e_n]` → `[]TInferred{...}`.
		// PR-F3 infers from the first element when it's an Int /
		// String / Bool literal; otherwise rejects (no sema yet).
		if v.ElemType != nil {
			g.b.WriteString("[]")
			if err := g.emitTypeExpr(v.ElemType); err != nil {
				return err
			}
		} else if elem, ok := g.semaSliceElem(v); ok {
			// Sema typed the literal (e.g. `[v]` from an Ident /
			// call element): use its element type directly.
			g.b.WriteString("[]")
			g.b.WriteString(elem)
		} else {
			// No sema info — fall back to first-literal inference.
			elem, err := inferSliceElemType(v.Items)
			if err != nil {
				return err
			}
			g.b.WriteString("[]")
			g.b.WriteString(elem)
		}
		g.b.WriteByte('{')
		for i, it := range v.Items {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitExpr(it); err != nil {
				return err
			}
		}
		g.b.WriteByte('}')
		return nil
	case *ast.Index:
		// `m[k]` where m is a Map<K, V> lowers to the wrapper's
		// internal `m.m[k]` direct map access — returns V's
		// Go zero value for a missing key (mirrors Go's map
		// semantics). `m.Get(k)` is the explicit-Option form
		// when the user wants the missing case to surface. The raw read
		// goes through the exported At accessor (not the unexported `m`
		// field) so the same emission works across the arilrt package
		// boundary in vendored mode.
		if id, ok := v.Receiver.(*ast.Ident); ok && (g.varKindOf(id) == "Map" || g.varKindOf(id) == "List") {
			// `l[i]` on a List lowers to `l.At(i)` (a Go bounds-checked slice
			// index inside the wrapper) — the same exported-accessor path as
			// Map's `m[k]`, so it works across the arilrt boundary in vendored mode.
			// When the element type is itself a reference container, `.At`'s
			// zero-value-on-miss is a nil pointer; wrap it in the Coalesce
			// helper so a miss yields the empty container, not nil (T13).
			kind := g.indexMapValueContainerKind(id)
			if kind != "" {
				g.b.WriteString(g.rt("Coalesce" + kind))
				g.b.WriteByte('(')
			}
			if err := g.emitExpr(id); err != nil {
				return err
			}
			g.b.WriteString(".At(")
			if err := g.emitExpr(v.Idx); err != nil {
				return err
			}
			g.b.WriteByte(')')
			if kind != "" {
				g.b.WriteByte(')')
			}
			return nil
		}
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteByte('[')
		if err := g.emitExpr(v.Idx); err != nil {
			return err
		}
		g.b.WriteByte(']')
		return nil
	case *ast.Slice:
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteByte('[')
		if v.Low != nil {
			if err := g.emitExpr(v.Low); err != nil {
				return err
			}
		}
		g.b.WriteByte(':')
		if v.High != nil {
			if err := g.emitExpr(v.High); err != nil {
				return err
			}
		}
		g.b.WriteByte(']')
		return nil
	case *ast.MatchExpr:
		return g.emitMatchAsExpr(v)
	case *ast.Block:
		return g.emitBlockAsExpr(v)
	case *ast.IfExpr:
		return g.emitIfExprAsValue(v)
	case *ast.ParenExpr:
		// Reproduce the author's grouping so Go preserves the same
		// operator precedence (`a * (b + c)` must not re-associate).
		g.b.WriteByte('(')
		if err := g.emitExpr(v.Inner); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case *ast.BraceLit:
		return g.emitBraceLit(v)
	case *ast.ClosureLit:
		return g.emitClosure(v)
	case *ast.TupleLit:
		return g.emitTupleLit(v)
	case *ast.TupleField:
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteString("._")
		g.b.WriteString(strconv.Itoa(v.Position))
		return nil
	case *ast.BreakExpr, *ast.ContinueExpr:
		// Diverging loop expressions lower to statements, not Go
		// expressions — they're handled in emitStmt. Reaching here
		// means one was used in value position (e.g. a value-arm
		// `match x { A => break }`), which v1 codegen does not lower.
		return fmt.Errorf("codegen: `break`/`continue` is not usable in value position")
	case *ast.Field:
		return g.emitField(v)
	case *ast.Call:
		return g.emitCall(v)
	case *ast.SpreadArg:
		// `...xs` lowers to Go's trailing `xs...` spread (ffi.md §Variadic).
		if err := g.emitExpr(v.Inner); err != nil {
			return err
		}
		g.b.WriteString("...")
		return nil
	case *ast.Binary:
		if err := g.emitExpr(v.Left); err != nil {
			return err
		}
		g.b.WriteByte(' ')
		g.b.WriteString(v.Op)
		g.b.WriteByte(' ')
		return g.emitExpr(v.Right)
	case *ast.Unary:
		g.b.WriteString(v.Op)
		return g.emitExpr(v.Operand)
	case *ast.ReturnExpr:
		// ReturnExpr is a DivergingExpr; in Go it must appear as
		// a statement (`return [value]`), not in an expression
		// context. The ExprStmt wrapper emitter writes the
		// statement form via emitReturnAsStatement directly, so
		// reaching this branch means a misuse (return in a
		// non-statement context) — emit clearly.
		return fmt.Errorf("codegen: return-expression used outside statement position")
	case *ast.TryExpr:
		// Expression-position `try` (call arg, operand, …) is lowered
		// by hoistExprTries, which pre-emits the early-return preamble
		// as a statement and records the unwrap temp here. The node's
		// value is the temp's payload `<tmp>.V`. A `try` that wasn't
		// hoisted sits in an unsupported frame (value-position
		// match/if/closure arm — a different return frame): error.
		if tmp, ok := g.tryHoist[v]; ok {
			g.b.WriteString(tmp)
			g.b.WriteString(".V")
			return nil
		}
		return g.tryExprErr()
	}
	return fmt.Errorf("codegen: unhandled expression %T", e)
}

func (g *gen) emitField(f *ast.Field) error {
	// `scope.context` lowers to the nearest enclosing scope's derived
	// context variable (the whole `recv.context` selector, not a Go
	// field access on it). Any other `scope.X` is rejected by sema, so
	// only `.context` reaches codegen here.
	if _, ok := f.Receiver.(*ast.ScopeRef); ok {
		return g.emitScopeContext(f.Span)
	}
	if err := g.emitExpr(f.Receiver); err != nil {
		return err
	}
	g.b.WriteByte('.')
	// Foreign-handle field access (ffi.md §ExternImpl) takes the Go
	// field name from its `@go` attribute, not the exported-Aril form.
	if fld, ok := g.externFieldOf(f); ok {
		g.b.WriteString(goRefMember(fld.Go, fld.Name))
		return nil
	}
	g.b.WriteString(g.goFieldName(f.Receiver, f.Name))
	return nil
}

// goFieldName maps a Aril *field-value* access `recv.name` to its Go
// spelling. A genuine user record/class field is EXPORTED
// (exportFieldName) so encoding/json can reach it; a stdlib-namespace
// value access (`os.args` → `os.Args`) keeps its binding rename, and
// `.error()` on the predeclared `error` builtin maps to Go's
// `error.Error()` (the PascalCase↔lowerCamel boundary; D14 footnote).
// Method-call selectors do NOT come through here — they use goMethodName
// (call.go), which stays lowercase, so methods remain unexported.
//
// The package-namespace check gates on the receiver's sema *symbol*
// (SymBuiltinModule), not its spelling — a local value that shadows a
// package name (`let sort = Sorter{…}`) is a user value whose fields
// must still export (the recurring name-match footgun: dispatch on the
// resolved symbol, never the spelling).
func (g *gen) goFieldName(receiver ast.Expr, name string) string {
	if name == "error" && g.isErrorBuiltinReceiver(receiver) {
		return "Error"
	}
	if id, ok := receiver.(*ast.Ident); ok && g.isBuiltinModule(id) {
		return mapFieldName(receiver, name)
	}
	// Value-handle field access — `resp.statusCode` on a stdlib handle typed by
	// sema (http.Response) takes the exported Go field name from the shared
	// binding.handleFields table (D37), not the generic exported-Aril form.
	if gf, ok := g.handleFieldGoName(receiver, name); ok {
		return gf
	}
	return exportFieldName(name)
}

// handleFieldGoName returns the exported Go struct-field name for `receiver.name`
// when sema typed receiver as a bound stdlib value-handle that exposes fields
// (http.Response), reading the shared binding.handleFields table. The field-axis
// mirror of handleMethodGoName.
func (g *gen) handleFieldGoName(receiver ast.Expr, name string) (string, bool) {
	if g.info == nil {
		return "", false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return "", false
	}
	hf, ok := binding.HandleFieldOf(named.N, name)
	if !ok {
		return "", false
	}
	return hf.GoName, true
}

// isDataFieldSelector reports whether `recv.name` names a *data field*
// (as opposed to a method) of recv's record/class type. A func-typed
// data field can be *called* — `handler.fn(x)` — and the callee is then
// an `*ast.Field` whose name must take the exported field spelling
// (goFieldName), not the lowercase method spelling, or it would not
// match the exported Go struct field. Records have only data fields;
// classes split fields vs methods; interfaces/containers/stdlib have
// no data fields reachable this way (→ false, method spelling).
func (g *gen) isDataFieldSelector(receiver ast.Expr, name string) bool {
	if g.info == nil {
		return false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return false
	}
	switch d := named.Decl.(type) {
	case *ast.ClassDecl:
		for _, fld := range d.Fields {
			if fld.Name == name {
				return true
			}
		}
	case *ast.TypeDecl:
		if rb, ok := d.Body.(*ast.RecordTypeBody); ok {
			for _, fld := range rb.Fields {
				if fld.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// goMethodName maps a Aril method-call selector `recv.name(...)` to its
// Go spelling. Container methods (Map / Set / Stack) take the EXPORTED
// spelling (`.get` → `.Get`) so vendored-mode code can call them across
// the arilrt package boundary; the same exported spelling is used inline
// for a single naming scheme. Other methods keep the pre-export
// behaviour (stdlib renames + the `error`→`Error` boundary, otherwise
// the verbatim lowercase name).
func (g *gen) goMethodName(receiver ast.Expr, name string) string {
	if name == "error" && (g.isErrorBuiltinReceiver(receiver) || g.isErrorImplementingClassReceiver(receiver)) {
		return "Error"
	}
	if g.isContainerTypedExpr(receiver) || g.isOptionResultTypedExpr(receiver) {
		return exportFieldName(name)
	}
	// A call on a class instance whose method implements a bound Go interface
	// (`h.serveHTTP` → `h.ServeHTTP`): the exported Go name comes from the
	// bound-interface table, mirroring the declaration-site classMethodGoName.
	if goName, ok := g.boundInterfaceMethodGoName(receiver, name); ok {
		return goName
	}
	// Value-handle method (`re.matchString` → `re.MatchString`): the receiver's
	// sema type is a bound stdlib handle Named, and the Go method name comes
	// from the shared binding table (D37), not the verbatim Aril name.
	if goName, ok := g.handleMethodGoName(receiver, name); ok {
		return goName
	}
	return mapFieldName(receiver, name)
}

// handleGoType returns the Go type spelling for a bound value-handle type
// (D37): the vendored/inline runtime selector for a runtime-backed
// handle (big.BigInt → arilrt.BigInt / BigInt), the verbatim Go spelling for an
// external one (regexp.Regexp → *regexp.Regexp). ok=false when `spelled` is not
// a bound handle type. Shared by the two type-emission sites (emitTypeExpr for
// annotations, goTypeFromSema for sema-derived positions).
func (g *gen) handleGoType(spelled string) (string, bool) {
	ht, ok := binding.HandleTypeOf(spelled)
	if !ok {
		return "", false
	}
	if ht.Runtime {
		return g.rt(ht.GoType), true
	}
	return ht.GoType, true
}

// boundInterfaceMethodGoName returns the exported Go method name for
// `receiver.name` when sema typed receiver as a class that implements a bound Go
// interface declaring `name` (`h.serveHTTP` → `ServeHTTP`). Mirrors the
// declaration-site classMethodGoName so a class method is spelled identically
// wherever it is called. ok=false when the receiver is not such a class.
func (g *gen) boundInterfaceMethodGoName(receiver ast.Expr, name string) (string, bool) {
	if g.info == nil {
		return "", false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return "", false
	}
	cd, ok := named.Decl.(*ast.ClassDecl)
	if !ok {
		return "", false
	}
	return boundInterfaceGoName(cd, name) // shared decl/call kernel (decl.go)
}

// handleMethodGoName returns the Go method name for `receiver.name` when
// sema typed receiver as a bound stdlib value-handle (regexp.Regexp, …),
// reading the shared binding.handleMethods table.
func (g *gen) handleMethodGoName(receiver ast.Expr, name string) (string, bool) {
	if g.info == nil {
		return "", false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return "", false
	}
	hm, ok := binding.HandleMethodOf(named.N, name)
	if !ok {
		return "", false
	}
	return hm.GoName, true
}

// handleMethodResultWrap reports whether the field-callee `fld` is a
// value-handle method whose curated return is `Result<…>` (net.Conn.read/
// write/close, net.Listener.accept) — the first handle methods to carry a
// Result. It returns the binding and whether the Result is unit-typed
// (`Result<unit, error>` ← a bare-`error` Go return → ResultUnit) vs a
// `(T, error)` pair (→ ResultOf). The plain handle-method path
// (handleMethodGoName) only renames the Go method, emitting the bare Go
// `(T, error)` call, which would not type as an arilrt.Result — so emitCall
// intercepts these before the generic method emit and applies the same lift
// the stdlib registry / extern paths use.
func (g *gen) handleMethodResultWrap(fld *ast.Field) (binding.HandleBinding, bool, bool) {
	if g.info == nil {
		return binding.HandleBinding{}, false, false
	}
	named, ok := g.info.Type[fld.Receiver].(*sema.Named)
	if !ok {
		return binding.HandleBinding{}, false, false
	}
	hm, ok := binding.HandleMethodOf(named.N, fld.Name)
	if !ok || !strings.HasPrefix(hm.Return, "Result<") {
		return binding.HandleBinding{}, false, false
	}
	return hm, true, hm.Return == "Result<unit, error>"
}

// isContainerTypedExpr reports whether sema typed receiver as one of the
// predeclared container types (Map / Set / Stack), whose methods are
// emitted with their exported Go spelling. Unlike isContainerReceiver
// (Ident-only, container-or-channel), this matches any expression and
// excludes channels.
func (g *gen) isContainerTypedExpr(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	switch g.info.Type[receiver].(type) {
	case *sema.Map, *sema.Set, *sema.Stack, *sema.List, *sema.AtomicPtr:
		// AtomicPtr methods (load/store/swap/compareAndSwap) cross the arilrt
		// boundary to the exported wrapper methods, like the container methods.
		return true
	}
	return false
}

// isOptionResultTypedExpr reports whether sema typed receiver as Option or
// Result, whose query/defaulting methods (isSome/isNone/unwrapOr, isOk/isErr)
// carry the exported Go spelling of the arilrt method set (builtins.md
// §Option methods / §Result methods).
func (g *gen) isOptionResultTypedExpr(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	switch g.info.Type[receiver].(type) {
	case *sema.Option, *sema.Result:
		return true
	}
	return false
}

// isResultReceiver reports whether sema typed receiver as Result — the gate
// for lowering `r.mapErr(f)` to the free MapErr helper (excludes Option, which
// has no mapErr).
func (g *gen) isResultReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	_, ok := g.info.Type[receiver].(*sema.Result)
	return ok
}

// isOptionReceiver reports whether sema typed receiver as Option — the gate for
// lowering `o.map(f)` to the free OptionMap helper.
func (g *gen) isOptionReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	_, ok := g.info.Type[receiver].(*sema.Option)
	return ok
}

// isDurationReceiver reports whether sema typed receiver as the time.Duration
// handle — the gate for lowering d.add/d.mul to Go operators (D37).
func (g *gen) isDurationReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	return ok && named.N == "time.Duration"
}

// isResponseWriterReceiver reports whether sema typed receiver as the
// http.ResponseWriter handle — gates the writeString convenience lowering.
func (g *gen) isResponseWriterReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	return ok && named.N == "http.ResponseWriter"
}

// isErrorBuiltinReceiver reports whether sema typed receiver as the
// predeclared `error` type (the Go-error binding boundary).
func (g *gen) isErrorBuiltinReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	b, ok := g.info.Type[receiver].(*sema.Builtin)
	return ok && b.N == "error"
}

// isErrorImplementingClassReceiver reports whether sema typed receiver as a
// class that `implements error` — its `.error()` call renames to Go's `.Error()`
// to match the method declaration's error→Error boundary (classImplementsError).
func (g *gen) isErrorImplementingClassReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return false
	}
	cd, ok := named.Decl.(*ast.ClassDecl)
	return ok && classImplementsError(cd)
}

// emitStringInterp lowers an interpolated string to a Go fmt.Sprintf call:
// the literal segments become the format string (each hole a `%v`, and a
// literal `%` doubled to `%%`), and the holes become the trailing
// arguments (lowering-go.md §String interpolation). A hole-free interp
// token never reaches here (the lexer only tags `${…}` strings).
func (g *gen) emitStringInterp(v *ast.StringInterpExpr) error {
	g.usedGoPkgs["fmt"] = true
	var format strings.Builder
	for i, part := range v.Parts {
		if i > 0 {
			format.WriteString("%v")
		}
		format.WriteString(strings.ReplaceAll(part, "%", "%%"))
	}
	g.b.WriteString("fmt.Sprintf(")
	g.b.WriteString(strconv.Quote(format.String()))
	for _, hole := range v.Holes {
		g.b.WriteString(", ")
		if err := g.emitExpr(hole); err != nil {
			return err
		}
	}
	g.b.WriteByte(')')
	return nil
}
