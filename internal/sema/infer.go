package sema

import (
	"strconv"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/binding"
)

// inferExpr returns the type of e, recording it in Info.Type and
// emitting any local typing diagnostic. A shape PR-C1 cannot type
// yet returns *Unknown. See docs/internals/sema.md §4 / §6.
func (c *checker) inferExpr(e ast.Expr) Type {
	if e == nil {
		return &Unit{}
	}
	var t Type
	switch v := e.(type) {
	case *ast.IntLitExpr:
		t = &Builtin{N: "int"}
	case *ast.FloatLitExpr:
		t = &Builtin{N: "float64"} // T-FloatLit
	case *ast.StringLitExpr:
		t = &Builtin{N: "string"}
	case *ast.StringInterpExpr:
		// Type each hole so its own member/arity checks fire (E0214 /
		// E0202 …) and the node is a concrete `string` — otherwise it
		// stays Unknown and a value-position interp (a match arm, an
		// if-branch) fails Go type inference (T-StringInterp).
		for _, h := range v.Holes {
			c.inferExpr(h)
		}
		t = &Builtin{N: "string"}
	case *ast.BoolLitExpr:
		t = &Builtin{N: "bool"}
	case *ast.RuneLitExpr:
		t = &Builtin{N: "rune"}
	case *ast.UnitLit:
		t = &Unit{}
	case *ast.ThisExpr:
		if c.curThis != nil {
			t = c.curThis
		} else {
			c.report("E0501", "`this` outside an instance-method body", v.Span)
			t = &Unknown{}
		}
	case *ast.Ident:
		t = c.inferIdent(v)
	case *ast.Call:
		t = c.inferCall(v)
	case *ast.SpreadArg:
		// A spread `...xs` has the type of the slice it forwards; its
		// validity (variadic target, element type) is checked by the
		// enclosing call (checkSpreadTarget / checkArgTypes).
		t = c.inferExpr(v.Inner)
	case *ast.Field:
		t = c.inferField(v)
	case *ast.Binary:
		t = c.inferBinary(v)
	case *ast.Unary:
		t = c.inferUnary(v)
	case *ast.ParenExpr:
		t = c.inferExpr(v.Inner)
	case *ast.TupleLit:
		comps := make([]Type, len(v.Components))
		for i, ce := range v.Components {
			comps[i] = c.inferExpr(ce)
		}
		t = &Tuple{Comps: comps}
	case *ast.TupleField:
		rt := c.inferExpr(v.Receiver)
		if tup, ok := rt.(*Tuple); ok && v.Position >= 0 && v.Position < len(tup.Comps) {
			t = tup.Comps[v.Position]
		} else {
			t = &Unknown{}
		}
	case *ast.BraceLit:
		t = c.inferBraceLit(v)
	case *ast.ClosureLit:
		t = c.inferClosure(v)
	case *ast.Block:
		t = c.inferBlock(v)
	case *ast.IfExpr:
		t = c.inferIfExpr(v)
	case *ast.MatchExpr:
		t = c.inferMatch(v)
	case *ast.ReturnExpr:
		c.checkReturn(v)
		t = &Never{}
	case *ast.BreakExpr:
		if c.loopDepth == 0 {
			c.report("E0404", "`break` outside a loop", v.Span)
		}
		t = &Never{}
	case *ast.ContinueExpr:
		if c.loopDepth == 0 {
			c.report("E0404", "`continue` outside a loop", v.Span)
		}
		t = &Never{}
	case *ast.TryExpr:
		inner := c.inferExpr(v.Inner)
		if c.curTryForbidden {
			c.report("E0402", "`try` outside a Result/Option-returning function", v.Span)
		}
		t = c.tryResultType(inner, v)
	case *ast.ScopeRef:
		c.checkScopeRef(v.Span)
		t = &Unknown{}
	case *ast.ScopeExpr:
		t = c.inferScope(v)
	case *ast.SpawnExpr:
		t = c.inferSpawn(v)
	case *ast.SliceLit:
		t = c.inferSliceLit(v)
	case *ast.Index:
		t = c.inferIndex(v)
	case *ast.Slice:
		recv := c.inferExpr(v.Receiver)
		c.expectInt(v.Low)
		c.expectInt(v.High)
		if _, ok := recv.(*Slice); ok {
			t = recv // s[lo:hi] : []T
		} else {
			t = &Unknown{}
		}
	default:
		t = &Unknown{}
	}
	c.info.Type[e] = t
	return t
}

// inferClosure types a closure literal as a Func. Param types come
// from annotations (Unknown when omitted in the short form); the body
// is checked with the closure's return type in scope, then the return
// type is the annotation, or the body's value type when omitted.
// isUnitOrNever reports whether t is the value-less unit type or the
// bottom Never — the two "no trailing value" outcomes of a block body.
func isUnitOrNever(t Type) bool {
	switch t.(type) {
	case *Unit, *Never:
		return true
	}
	return false
}

func (c *checker) inferClosure(cl *ast.ClosureLit) Type {
	expect := c.closureExpect[cl]
	params := make([]Type, len(cl.Params))
	for i, prm := range cl.Params {
		switch {
		case prm.DeclType != nil:
			params[i] = c.typeFromExpr(prm.DeclType)
		case expect != nil && i < len(expect.Params) && concrete(expect.Params[i]):
			// Unannotated short-closure parameter typed from call
			// context (T-Closure). The resolve pass declared the
			// param symbol as Unknown; refine it so the body's
			// references (`a.start`) type against the real element type.
			params[i] = expect.Params[i]
			if sym := c.info.Def[prm]; sym != nil {
				sym.Type = expect.Params[i]
			}
		default:
			params[i] = &Unknown{}
		}
	}
	savedReturn, savedThis, savedForbidden := c.curReturn, c.curThis, c.curTryForbidden
	// A closure boundary breaks the lexical `scope` enclosure: the
	// closure may be invoked outside the scope, so a `spawn` in its
	// body is not registered on the scope's group (E0405 applies).
	savedScopeDepth := c.scopeDepth
	c.scopeDepth = 0
	savedAcc := c.returnAcc
	var acc []Type
	var ret Type
	if cl.ReturnType != nil {
		ret = c.typeFromExpr(cl.ReturnType)
		c.curReturn = ret
		c.curTryForbidden = c.definitelyNotTryable(cl.ReturnType)
		c.returnAcc = nil // annotated: the type is fixed, don't accumulate
	} else {
		c.curReturn = &Unknown{}
		c.curTryForbidden = false
		c.returnAcc = &acc // un-annotated: collect `return` value types
	}
	bodyVal := c.inferBlock(cl.Body)
	c.curReturn, c.curThis, c.curTryForbidden = savedReturn, savedThis, savedForbidden
	c.scopeDepth = savedScopeDepth
	c.returnAcc = savedAcc
	if ret == nil {
		ret = bodyVal
		// A block body that yields only via `return` has no trailing value
		// (bodyVal is unit/Never); recover the real result from the
		// collected return types (T-Closure-Block).
		if len(acc) > 0 && isUnitOrNever(bodyVal) {
			ret = acc[0]
		}
	}
	return &Func{Params: params, Return: ret}
}

// inferScope types a `scope<T, E>(parent?) { body }` expression as
// Result<T, E> (T-ScopeExpr). v1 restricts E = error (E0407). The
// body is checked with scopeDepth raised so a `spawn` inside is
// legal; the scope itself is the join point.
func (c *checker) inferScope(s *ast.ScopeExpr) Type {
	if len(s.TypeArgs) == 2 {
		et := c.typeFromExpr(s.TypeArgs[1])
		if concrete(et) && !isErrorBuiltin(et) {
			c.report("E0407", "`scope` error parameter must be `error` in v1", s.TypeArgs[1].NodeSpan())
		}
	}
	if s.Parent != nil {
		c.inferExpr(s.Parent)
	}
	// The scope body is its own frame, distinct from any enclosing spawn:
	// reset curSpawnFrame (so a `try` here is not mis-attributed to a
	// spawn) and forbid a *direct* `try` in the scope body — codegen has
	// no scope-frame bail (the try would mis-target the outer function's
	// Result), so reject it at sema (E0402) rather than miscompile. A
	// `spawn` inside re-permits `try` (inferSpawn clears the flag).
	savedForbidden, savedSpawnFrame := c.curTryForbidden, c.curSpawnFrame
	c.curTryForbidden = true
	c.curSpawnFrame = false
	c.scopeDepth++
	c.checkBlock(s.Body)
	c.scopeDepth--
	c.curTryForbidden, c.curSpawnFrame = savedForbidden, savedSpawnFrame
	// The scope evaluates to Result<T, E>, with T / E taken from the
	// `scope<T, E>` type arguments when present (T-ScopeExpr).
	var t, e Type = &Unknown{}, &Unknown{}
	if len(s.TypeArgs) == 2 {
		t = c.typeFromExpr(s.TypeArgs[0])
		e = c.typeFromExpr(s.TypeArgs[1])
	}
	return &Result{T: t, E: e}
}

// checkScopeRef validates a `scope`-as-value reference: it is legal only
// inside the lexical body of a scope block (scopeDepth > 0, which the
// resolver reaches through any enclosing spawn / block), else E0601.
func (c *checker) checkScopeRef(span ast.Span) {
	if c.scopeDepth == 0 {
		c.report("E0601", "`scope` outside a `scope` block", span)
	}
}

// inferSpawn types `spawn { body }` as unit (T-Spawn). E0405 when it
// is not lexically inside a `scope` body. The body is checked
// normally; its `return Ok(())` / `return Err(e)` are converted to
// the group's error channel at lowering.
func (c *checker) inferSpawn(s *ast.SpawnExpr) Type {
	if c.scopeDepth == 0 {
		c.report("E0405", "`spawn` outside a `scope` block", s.Span)
	}
	// A spawn body is an implicit `Result<unit, error>`-returning frame
	// (it must `return Ok(())`, and its error feeds the group), so `try`
	// inside it is permitted regardless of the enclosing function's
	// return type — drop the try-forbidden flag for the body (T-Try). The
	// frame is a Result, so a `try` on an Option there is ill-formed
	// (E0408, flagged via curSpawnFrame in tryResultType).
	savedForbidden, savedSpawnFrame := c.curTryForbidden, c.curSpawnFrame
	c.curTryForbidden = false
	c.curSpawnFrame = true
	c.checkBlock(s.Body)
	c.curTryForbidden, c.curSpawnFrame = savedForbidden, savedSpawnFrame
	return &Unit{}
}

// isErrorBuiltin reports whether t is the predeclared `error` type.
func isErrorBuiltin(t Type) bool {
	b, ok := t.(*Builtin)
	return ok && b.N == "error"
}

// inferBraceLit types a brace literal. Record literals resolve to
// their nominal type and check each field value against the declared
// field type (E0201). Map / Set / Stack literals resolve to their
// container type from the type-args, checking each entry against the
// element / key+value type (inferContainerBraceLit).
func (c *checker) inferBraceLit(b *ast.BraceLit) Type {
	// Map / Set / Stack brace literals resolve from their type-args,
	// not a nominal record decl (collections.go).
	if name := containerBraceName(b); name != "" {
		return c.inferContainerBraceLit(b, name)
	}
	rt := c.typeFromExpr(b.TypeName)
	// An opaque handle has no visible layout — it cannot be built
	// from a literal (ffi.md §ExternType).
	if isOpaqueHandle(rt) {
		c.report("E1001", "Cannot construct opaque foreign handle "+rt.String()+" — obtain it from an extern function", b.Span)
		return &Unknown{}
	}
	// A bound stdlib handle: a Constructable one is empty-only (`sync.Mutex{}`);
	// an obtain-only one (regexp.Regexp) is never brace-built. Diagnose the misuse
	// here in .aril coordinates (E0218) rather than leaking a codegen bail string
	// (D10; lowering-go §Brace literals).
	if named, ok := rt.(*Named); ok && binding.IsHandleType(named.N) {
		if !binding.IsConstructableHandle(named.N) {
			c.report("E0218", "Stdlib handle `"+named.N+"` is obtain-only — obtain it from its constructor, not a `{}` literal", b.Span)
			return &Unknown{}
		}
		if len(b.Entries) != 0 {
			c.report("E0218", "Stdlib handle `"+named.N+"` takes no fields — construct it as `"+named.N+"{}`", b.Span)
			return &Unknown{}
		}
		return rt // `sync.Mutex{}` zero-construction; its method set resolves off rt.
	}
	for _, e := range b.Entries {
		switch en := e.(type) {
		case *ast.RecordEntry:
			vt := c.inferExpr(en.Value)
			if ft := c.recordFieldType(rt, en.Name); ft != nil {
				if !c.fits(ft, en.Value, vt) {
					c.report("E0201", "Type mismatch — field "+en.Name+" expects "+ft.String()+", got "+vt.String(), en.Value.NodeSpan())
				}
			} else if isRecordType(rt) {
				// Unknown field on a known record — catch it here in
				// .aril coordinates rather than leaking a go/types error.
				c.report("E0201", "Record "+rt.String()+" has no field "+en.Name, en.Span)
			}
		case *ast.MapEntry:
			c.inferExpr(en.Key)
			c.inferExpr(en.Value)
		case *ast.SetEntry:
			c.inferExpr(en.Value)
		}
	}
	if b.Kind == ast.BraceRecord {
		return rt
	}
	// An empty `T{}` (no entries → BraceUnknown) on a class/record is
	// zero-construction: type it as T so a mismatch is caught in .aril terms
	// (E0201) instead of leaking a go/types error at build (D10). Consistent
	// with partial record literals, which already zero-fill omitted fields
	// (codegen lowers to `&T{}` / `T{}`; see lowering-go.md §Brace literals).
	if len(b.Entries) == 0 && isClassOrRecordNamed(rt) {
		return rt
	}
	// (A constructable stdlib handle `sync.Mutex{}` is typed above, alongside the
	// E0218 handle-brace-literal diagnostics.)
	return &Unknown{}
}

// isClassOrRecordNamed reports whether t is a Named type whose decl is a class
// or a record — the empty-literal `T{}` zero-construction targets.
func isClassOrRecordNamed(t Type) bool {
	named, ok := t.(*Named)
	if !ok {
		return false
	}
	if _, ok := named.Decl.(*ast.ClassDecl); ok {
		return true
	}
	return isRecordType(t)
}

// isRecordType reports whether t is a nominal record (a Named whose
// decl is a RecordTypeBody).
func isRecordType(t Type) bool {
	named, ok := t.(*Named)
	if !ok {
		return false
	}
	td, ok := named.Decl.(*ast.TypeDecl)
	if !ok {
		return false
	}
	_, ok = td.Body.(*ast.RecordTypeBody)
	return ok
}

// recordFieldType returns the declared type of field `name` on a
// record type, or nil when t is not a record / has no such field.
func (c *checker) recordFieldType(t Type, name string) Type {
	named, ok := t.(*Named)
	if !ok {
		return nil
	}
	td, ok := named.Decl.(*ast.TypeDecl)
	if !ok {
		return nil
	}
	rb, ok := td.Body.(*ast.RecordTypeBody)
	if !ok {
		return nil
	}
	for _, f := range rb.Fields {
		if f.Name == name {
			return c.typeFromExpr(f.DeclType)
		}
	}
	return nil
}

func (c *checker) inferIdent(id *ast.Ident) Type {
	if id.Name == "_" {
		return &Unknown{}
	}
	if sym := c.info.Symbol[id]; sym != nil {
		switch sym.Kind {
		case SymTypeParam, SymClass, SymTypeDecl, SymBuiltinType, SymInterface:
			// A type parameter or named type used where a value is
			// expected (name-resolution.md §Generic type-argument
			// resolution / E0108). Legitimate qualifier positions — a
			// call callee (`Counter(...)`, `int(x)`), a field/method
			// receiver (`Counter.new`, `Tree.Leaf`), a brace literal
			// (`Counter{...}`) — never reach here: inferCall skips Ident
			// callees, inferReceiver handles qualifiers, brace literals
			// carry a NamedType (typed via typeFromExpr). So this is a
			// genuine value position.
			c.report("E0108", "Type used as value — `"+id.Name+"` is a type, not a value", id.Span)
			return &Unknown{}
		}
		return symValueType(sym)
	}
	return &Unknown{}
}

// inferReceiver types a field / method receiver. A namespace or type
// qualifier (`fmt.x`, `Counter.new`, `Tree.Leaf`) sits in value-ish
// position but is not a value, so it must not trigger E0108 (which
// inferIdent would fire) — the member-access dispatch handles it.
func (c *checker) inferReceiver(e ast.Expr) Type {
	if id, ok := e.(*ast.Ident); ok {
		if sym := c.info.Symbol[id]; sym != nil {
			switch sym.Kind {
			case SymBuiltinModule, SymClass, SymTypeDecl, SymBuiltinType, SymInterface, SymTypeParam:
				return symValueType(sym)
			}
		}
	}
	return c.inferExpr(e)
}

// inferCall types a call and checks argument types against the
// callee's parameters (E0201) on top of the arity check (E0202).
func (c *checker) inferCall(call *ast.Call) Type {
	// An Ident callee is typed by the symbol switch below, not as a
	// value — skip inferExpr so a class / type-name callee (`Box(...)`,
	// `int(x)`) is not misread as a value (E0108). A Field callee
	// (`fmt.println`, `Box.new`) still routes through inferField, which
	// uses inferReceiver to keep its qualifier from firing E0108.
	if _, isIdent := call.Callee.(*ast.Ident); !isIdent {
		c.inferExpr(call.Callee)
	}
	// A closure-taking stdlib binding (`sort.sorted(s, less)`) types
	// its comparator's omitted params from the slice element type
	// before the generic arg loop runs (so the closure body checks
	// against the real element type, not Unknown).
	if rt, handled := c.inferClosureBinding(call); handled {
		return rt
	}
	// `r.mapErr(f)` — the Err-conversion combinator (builtins.md §Result
	// methods). Its result type `Result<T, E2>` depends on the closure's
	// return E2, so it can't be a fixed containerMethodType Func; typed here
	// like inferClosureBinding, pre-typing the handler's param as the
	// receiver's E (T-Result-MapErr).
	if rt, handled := c.inferResultMapErr(call); handled {
		return rt
	}
	// `mapErr` on a Result with the wrong arity: it is a real method (exactly
	// one handler arg), but inferResultMapErr only claims the 1-arg shape, so a
	// 0- or 2-arg call would otherwise leak a go/types error from the MapErr
	// helper. Report arity in Aril coordinates (E0202); result stays Unknown.
	if c.isMapErrArityMiss(call) {
		c.report("E0202", "Wrong arity in call to mapErr: expects 1 argument (a handler `(e) => …`), got "+strconv.Itoa(len(call.Args)), call.Span)
	}
	args := make([]Type, len(call.Args))
	for i, a := range call.Args {
		args[i] = c.inferExpr(a)
	}
	c.checkCallArity(call)
	c.checkSpreadTarget(call)

	ret := Type(&Unknown{})
	if id, ok := call.Callee.(*ast.Ident); ok {
		if sym := c.info.Symbol[id]; sym != nil {
			switch sym.Kind {
			case SymFunc, SymMethod, SymExternFunc:
				if fn, ok := sym.Type.(*Func); ok {
					if len(fn.TypeParams) > 0 {
						fn = c.instantiate(fn, call, args)
					}
					c.checkArgTypes(fn, args, call.Args, sym.Name)
					ret = fn.Return
				}
			case SymExternType:
				// An opaque handle cannot be constructed by Aril — only
				// returned from an extern function (ffi.md §ExternType).
				c.report("E1001", "Cannot construct opaque foreign handle "+sym.Name+" — obtain it from an extern function", call.Span)
			case SymClass:
				ret = c.checkConstructor(sym, args, call.Args)
			case SymUserVariant:
				// Payload-variant constructor: its value is the sum.
				ret = sym.Type
			case SymBuiltinType:
				ret = c.inferBuiltinTypeCall(id.Name, call, args)
			case SymBuiltinFunc:
				ret = c.inferBuiltinFuncCall(id.Name, call, args)
			}
		}
	} else if f, ok := call.Callee.(*ast.Field); ok {
		// Stdlib binding `pkg.method(args)` — its modelled return type
		// (interim sema binding table; bindings.go) so a match/try over
		// it is typed rather than Unknown.
		if bt := c.bindingCallReturn(f); bt != nil {
			ret = bt
		} else if gt := c.genericBindingReturn(call, f); gt != nil {
			// `fmt.scan<T>()` family — result type from call type-args.
			ret = gt
		} else if ct := c.staticContainerCtor(f, call); ct != nil {
			// Static container constructor `Map<K,V>.new()` /
			// `Set<T>.new()` / `Set<T>.from(..)` / `Stack<T>.new()`:
			// the type arguments bind to the container, carried on the
			// Call. Produces the structured container type.
			ret = ct
		} else if fn, ok := c.info.Type[f].(*Func); ok {
			// Method call `recv.m(args)`. inferField already typed
			// the callee as the method's Func when the receiver is a
			// class / extern / interface / value-handle. Check arity
			// (E0202) then the per-argument types (E0201).
			c.checkMethodArity(fn, call, f.Name)
			c.checkArgTypes(fn, args, call.Args, f.Name)
			ret = fn.Return
		}
	}
	return ret
}

// inferClosureBinding types a stdlib binding that takes a comparator
// closure whose parameter types come from the receiver slice's element
// type (`sort.sorted(s, less)` — binding-surface §sort): `less` is
// `(T, T) => bool` over the element `T`. It pre-types the closure
// argument's omitted params via closureExpect so the body checks
// against the real element type, then infers the args. Returns
// (resultType, true) when it handled the call; (nil, false) otherwise.
func (c *checker) inferClosureBinding(call *ast.Call) (Type, bool) {
	f, ok := call.Callee.(*ast.Field)
	if !ok {
		return nil, false
	}
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if sym := c.info.Symbol[recv]; sym == nil || sym.Kind != SymBuiltinModule || recv.Name != "sort" {
		return nil, false
	}
	if (f.Name != "sorted" && f.Name != "sortedBy") || len(call.Args) != 2 {
		return nil, false
	}
	sliceT := c.inferExpr(call.Args[0])
	elem := Type(&Unknown{})
	if s, ok := sliceT.(*Slice); ok {
		elem = s.Elem
	}
	if cl, ok := call.Args[1].(*ast.ClosureLit); ok {
		// sorted: `(T, T) => bool` comparator. sortedBy: `(T) => K` key
		// extractor (one param, K inferred from the body).
		if f.Name == "sorted" {
			c.closureExpect[cl] = &Func{Params: []Type{elem, elem}, Return: &Builtin{N: "bool"}}
		} else {
			c.closureExpect[cl] = &Func{Params: []Type{elem}, Return: &Unknown{}}
		}
	}
	c.inferExpr(call.Args[1])
	// The result preserves the input slice type — `sorted` returns a
	// new slice of the same element type (immutability of the input).
	if _, ok := sliceT.(*Slice); ok {
		return sliceT, true
	}
	return &Slice{Elem: elem}, true
}

// inferResultMapErr types `r.mapErr(f)` — the Err-conversion combinator that
// lets `try` cross an error-type boundary (builtins.md §Result methods,
// T-Result-MapErr). The handler `f: (E) => E2` transforms the Err payload; the
// call yields `Result<T, E2>`, its E2 read from the handler's inferred return.
// The receiver was already typed by inferCall's callee walk (info.Type is
// populated), so it is read from cache rather than re-inferred. Returns
// (resultType, true) when it handled the call; (nil, false) otherwise —
// non-Result receivers fall through to the ordinary member path.
func (c *checker) inferResultMapErr(call *ast.Call) (Type, bool) {
	f, ok := call.Callee.(*ast.Field)
	if !ok || f.Name != "mapErr" || len(call.Args) != 1 {
		return nil, false
	}
	res, ok := c.info.Type[f.Receiver].(*Result)
	if !ok {
		return nil, false
	}
	// Pre-type an unannotated handler param as the receiver's E, so its body
	// checks against the real error type (mirrors inferClosureBinding).
	if cl, ok := call.Args[0].(*ast.ClosureLit); ok {
		c.closureExpect[cl] = &Func{Params: []Type{res.E}, Return: &Unknown{}}
	}
	argT := c.inferExpr(call.Args[0])
	e2 := Type(&Unknown{})
	if fn, ok := argT.(*Func); ok && fn.Return != nil {
		e2 = fn.Return
	}
	return &Result{T: res.T, E: e2}, true
}

// isMapErrArityMiss reports whether call is `r.mapErr(...)` on a Result receiver
// with an argument count other than 1 — the shape inferResultMapErr does not
// claim (it requires exactly one handler). The receiver type is read from cache
// (inferCall's callee walk already typed it). The 1-arg shape returns false here
// (it is handled by inferResultMapErr); this only catches the 0-/2+-arg misuse.
func (c *checker) isMapErrArityMiss(call *ast.Call) bool {
	f, ok := call.Callee.(*ast.Field)
	if !ok || f.Name != "mapErr" || len(call.Args) == 1 {
		return false
	}
	_, ok = c.info.Type[f.Receiver].(*Result)
	return ok
}

// staticMethodType returns the Func type of a user-class static method
// referenced as `ClassName.method` (e.g. `DSU.new`), or nil when f is
// not such a reference. The class-name receiver resolves to SymClass,
// whose symValueType is Unknown (a class is not a value), so the
// instance-member path in inferField cannot type it; this resolves the
// static method off the class declaration directly. Instance methods
// (m.IsStatic == false) are left to the value path.
func (c *checker) staticMethodType(f *ast.Field) *Func {
	id, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return nil
	}
	sym := c.info.Symbol[id]
	if sym == nil || sym.Kind != SymClass {
		return nil
	}
	cd, ok := sym.Decl.(*ast.ClassDecl)
	if !ok {
		return nil
	}
	for _, m := range cd.Methods {
		if m.IsStatic && m.Name == f.Name {
			return c.methodSigType(m)
		}
	}
	return nil
}

// staticContainerCtor recognises a predeclared-container static
// constructor (`Map<K,V>.new()`, `Set<T>.new()/from()`,
// `Stack<T>.new()`) and returns the structured container type, or
// nil when f is not such a call.
func (c *checker) staticContainerCtor(f *ast.Field, call *ast.Call) Type {
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return nil
	}
	if sym := c.info.Symbol[recv]; sym == nil || sym.Kind != SymBuiltinType {
		return nil
	}
	switch recv.Name {
	case "Map":
		if len(call.TypeArgs) == 2 {
			return &Map{Key: c.typeFromExpr(call.TypeArgs[0]), Val: c.typeFromExpr(call.TypeArgs[1])}
		}
	case "Set":
		if len(call.TypeArgs) == 1 {
			return &Set{Elem: c.typeFromExpr(call.TypeArgs[0])}
		}
	case "Stack":
		if len(call.TypeArgs) == 1 {
			return &Stack{Elem: c.typeFromExpr(call.TypeArgs[0])}
		}
	}
	return nil
}

// checkConstructor types a `ClassName(args)` constructor call,
// checking each argument against the corresponding field type.
func (c *checker) checkConstructor(sym *Symbol, args []Type, argNodes []ast.Expr) Type {
	cd, ok := sym.Decl.(*ast.ClassDecl)
	if !ok {
		return &Unknown{}
	}
	params := make([]Type, len(cd.Fields))
	for i, f := range cd.Fields {
		params[i] = c.typeFromExpr(f.DeclType)
	}
	c.checkArgTypes(&Func{Params: params}, args, argNodes, cd.Name)
	return &Named{N: cd.Name, Decl: cd}
}

// checkArgTypes fires E0201 per positional argument whose type
// disagrees with the parameter. Length mismatches are E0202's
// job; this only compares the overlapping prefix.
func (c *checker) checkArgTypes(fn *Func, args []Type, nodes []ast.Expr, callee string) {
	params := fn.Params
	argMismatch := func(i int, want Type) {
		c.report("E0201",
			"Type mismatch — argument "+strconv.Itoa(i+1)+" to "+callee+
				" expects "+want.String()+", got "+args[i].String(),
			nodes[i].NodeSpan())
	}
	if !fn.Variadic {
		n := len(params)
		if len(args) < n {
			n = len(args)
		}
		for i := 0; i < n; i++ {
			if !c.fits(params[i], nodes[i], args[i]) {
				argMismatch(i, params[i])
			}
		}
		return
	}
	// Variadic callee: the final parameter is the slice `[]T` it
	// accepts (ffi.md §Variadic). Fixed parameters check positionally;
	// the trailing arguments each check against the element type T,
	// unless one spread argument `...xs` supplies the whole `[]T`.
	nfixed := len(params) - 1
	for i := 0; i < nfixed && i < len(args); i++ {
		if !c.fits(params[i], nodes[i], args[i]) {
			argMismatch(i, params[i])
		}
	}
	var elem Type = &Unknown{}
	if s, ok := params[nfixed].(*Slice); ok {
		elem = s.Elem
	}
	for i := nfixed; i < len(args); i++ {
		if sp, ok := nodes[i].(*ast.SpreadArg); ok {
			// `...xs` forwards a slice: it must fit the `[]T` parameter.
			if !c.fits(params[nfixed], sp.Inner, args[i]) {
				c.report("E0201",
					"Type mismatch — spread argument to "+callee+
						" expects "+params[nfixed].String()+", got "+args[i].String(),
					nodes[i].NodeSpan())
			}
			continue
		}
		if !c.fits(elem, nodes[i], args[i]) {
			argMismatch(i, elem)
		}
	}
}

// inferField types member access `recv.name`: a class field gives
// its declared type, a class method gives its Func type. Module
// access and everything else stays Unknown for PR-C1.
func (c *checker) inferField(f *ast.Field) Type {
	// `scope.context` — the cancellable context of the nearest enclosing
	// scope (name-resolution.md §Special names). The ScopeRef receiver
	// carries the E0601 (scope-outside-a-block) check. The context's type
	// is Unknown in v1, which fits the `context.Context` parameter
	// positions it feeds; bare `scope.<other>` is likewise Unknown.
	if sr, ok := f.Receiver.(*ast.ScopeRef); ok {
		c.checkScopeRef(sr.Span)
		return &Unknown{}
	}
	// Static method on a class name (`Box.new`, `DSU.new`) — the
	// receiver names the class (SymClass), not a value, so the value
	// path below (which needs a *Named receiver) can't reach it. Look
	// the static method up on the class decl and give it its Func type.
	if st := c.staticMethodType(f); st != nil {
		return st
	}
	recv := c.inferReceiver(f.Receiver)
	// Channel methods (`.send` / `.recv` / `.tryRecv` / `.close`)
	// dispatch on the channel kind, not on a Named declaration.
	if ct := channelMethodType(recv, f.Name); ct != nil {
		return ct
	}
	// Predeclared-container methods (`Map`/`Set`/`Stack`/`[]T`)
	// dispatch on the container kind, not on a Named declaration
	// (T-Container-Method).
	if ct := containerMethodType(recv, f.Name); ct != nil {
		return ct
	}
	named, ok := recv.(*Named)
	if !ok {
		// Unbound member on a bound stdlib namespace (`strings.foo`): report
		// E0217 rather than leaking a raw `go build` "undefined: pkg.foo" (D10).
		// architecture.md §binding subsystem.
		if c.unboundStdlibMember(f) {
			return &Unknown{}
		}
		// A builtin receiver with a fully-known method set (a container/sum/
		// channel, or a closed-method-set primitive like int/string/error) —
		// its valid methods resolved via channelMethodType / containerMethodType
		// above, so a miss here is a real unknown-member error. Report it in Aril
		// coordinates (E0214) rather than falling through to Unknown, which leaks
		// a go/types `type … has no field or method` against generated Go (D10).
		// Sound-over-complete (D38): only fully-known kinds fire — a bare type
		// parameter, Any/Dynamic, or an Unknown receiver stays silent.
		if builtinMemberMiss(recv, f.Name) {
			c.report("E0214", "Type "+recv.String()+" has no member `"+f.Name+"`", f.Span)
		}
		return &Unknown{}
	}
	// Value-handle method access — `re.matchString(s)` on a stdlib handle
	// type (regexp.Regexp, …). The method set is a hand-curated binding
	// table (binding.handleMethods) shared with codegen, keyed on the
	// handle's Aril type spelling (D37). Give the method its Func
	// so the call is arg-checked and typed.
	if hm, ok := binding.HandleMethodOf(named.N, f.Name); ok {
		return handleMethodSigType(hm)
	}
	// Record field access — `p.x` on a record type.
	if ft := c.recordFieldType(named, f.Name); ft != nil {
		return ft
	}
	// Interface method access — `f.method` gives the method's Func,
	// resolved through the `extends` chain (an interface inherits the
	// method set of every interface it extends; D14).
	if id, ok := named.Decl.(*ast.InterfaceDecl); ok {
		if m := c.interfaceMethodSig(id, f.Name); m != nil {
			return c.interfaceMethodType(m)
		}
		return c.unknownMember(named, f)
	}
	// Opaque-handle member access — a method gives its Func, a field
	// gives its declared type, both from the `extern impl` (ffi.md).
	if _, ok := named.Decl.(*ast.ExternTypeDecl); ok {
		if ei := c.externImpls[named.N]; ei != nil {
			for _, m := range ei.Methods {
				if m.Name == f.Name {
					return c.externMethodSigType(m)
				}
			}
			for _, fld := range ei.Fields {
				if fld.Name == f.Name {
					return c.typeFromExpr(fld.DeclType)
				}
			}
		}
		return c.unknownMember(named, f)
	}
	cd, ok := named.Decl.(*ast.ClassDecl)
	if !ok {
		return c.unknownMember(named, f)
	}
	for _, fld := range cd.Fields {
		if fld.Name == f.Name {
			return c.typeFromExpr(fld.DeclType)
		}
	}
	for _, m := range cd.Methods {
		if m.Name == f.Name {
			return c.methodSigType(m)
		}
	}
	return c.unknownMember(named, f)
}

// unboundStdlibMember reports E0217 when `pkg.name` accesses a member that is
// not bound on a shipped stdlib namespace, and returns whether it fired. Gated
// on the resolved SymBuiltinModule symbol (shadow-safe: a value or `extern`
// binding sharing the name is a different symbol kind, so it never fires) and
// on binding.IsMember (sound-over-complete: a member missing from the binding
// tables stays silent — leaks as before — never a false reject of a working
// call). architecture.md §binding subsystem.
func (c *checker) unboundStdlibMember(f *ast.Field) bool {
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok {
		return false
	}
	sym := c.info.Symbol[recv]
	if sym == nil || sym.Kind != SymBuiltinModule {
		return false
	}
	if binding.IsMember(recv.Name, f.Name) {
		return false
	}
	c.report("E0217", "Module `"+recv.Name+"` has no bound member `"+f.Name+"`", f.Span)
	return true
}

// unknownMember reports an Aril-coordinate diagnostic (E0214) when a
// member access `recv.name` misses on a *concrete* Named receiver — a
// user class / record / interface, an opaque `extern` handle, or a bound
// stdlib value-handle (D37). Without this the miss falls through to
// Unknown and the go/types backend reports `type … has no field or
// method …` against generated Go, leaking the Go spelling (a D10
// violation). Generic type parameters (Decl == nil and not a handle) and
// receivers of Unknown type stay silent — their member set is not known
// here. Returns Unknown so inference continues past the reported miss.
func (c *checker) unknownMember(named *Named, f *ast.Field) Type {
	if isConcreteNamed(named) {
		c.report("E0214",
			"Type "+named.N+" has no member `"+f.Name+"`", f.Span)
	}
	return &Unknown{}
}

// isConcreteNamed reports whether a Named receiver has a fully known
// member set: a user class / interface / record, an opaque `extern`
// handle, or a bound stdlib value-handle. A bare type parameter
// (`&Named{N}` with a nil Decl, resolve.go) is NOT concrete — its member
// set depends on the instantiation, so a miss there is not a user error.
func isConcreteNamed(named *Named) bool {
	switch d := named.Decl.(type) {
	case *ast.ClassDecl, *ast.InterfaceDecl, *ast.ExternTypeDecl:
		return true
	case *ast.TypeDecl:
		_, isRecord := d.Body.(*ast.RecordTypeBody)
		return isRecord
	}
	return binding.IsHandleType(named.N)
}

func (c *checker) inferBinary(b *ast.Binary) Type {
	lt := c.inferExpr(b.Left)
	rt := c.inferExpr(b.Right)
	lt, rt = c.adaptIntLiteralOperands(b, lt, rt)
	switch b.Op {
	case "+":
		// `+` is numeric addition or string concatenation.
		if isString(lt) || isString(rt) {
			c.expectSame(lt, rt, b, "string concatenation")
			return &Builtin{N: "string"}
		}
		return c.numericResult(lt, rt, b)
	case "-", "*", "/", "%":
		return c.numericResult(lt, rt, b)
	case "==", "!=":
		// Equality demands a comparable type (T-Cmp); class
		// operands route to refEq, collections / funcs are not
		// comparable at all.
		if (concrete(lt) && !comparable(lt)) || (concrete(rt) && !comparable(rt)) {
			if isOptionOrResult(lt) || isOptionOrResult(rt) {
				// An Option / Result is inspected with `match`, never
				// compared with `==` (the natural TS `x === null` instinct
				// on an Option, or `refEq` — both wrong here). D10 / E0401.
				c.report("E0401", "`"+b.Op+"` cannot compare Option / Result values — use `match` to inspect the case (e.g. `match o { Some(v) => ..., None => ... }`)", b.Span)
			} else {
				c.report("E0401", "`"+b.Op+"` on non-comparable type — compare field-wise, or use `refEq` for class identity", b.Span)
			}
		} else {
			c.expectSame(lt, rt, b, "comparison")
		}
		return &Builtin{N: "bool"}
	case "<", "<=", ">", ">=":
		// Ordering-domain enforcement (Ord) lands with a later PR;
		// here we only require the two operands to agree.
		c.expectSame(lt, rt, b, "comparison")
		return &Builtin{N: "bool"}
	case "&&", "||":
		// Dynamic / Any operands are a boundary concern, not a
		// bool-operand mismatch — consistent with the numeric /
		// comparison paths.
		if concrete(lt) && !isBool(lt) && !isDynamic(lt) && !isAny(lt) {
			c.report("E0201", "Type mismatch — `"+b.Op+"` expects bool, got "+lt.String(), b.Left.NodeSpan())
		}
		if concrete(rt) && !isBool(rt) && !isDynamic(rt) && !isAny(rt) {
			c.report("E0201", "Type mismatch — `"+b.Op+"` expects bool, got "+rt.String(), b.Right.NodeSpan())
		}
		return &Builtin{N: "bool"}
	default:
		return &Unknown{}
	}
}

// adaptIntLiteralOperands narrows an integer-literal operand to the
// other operand's concrete integer type (type-system.md §Literals):
// `b == 0` on a `byte`, `r + 1` on a `rune`. Returns the adjusted
// operand types and records the narrowed type / range-checks the
// literal. When both operands are literals nothing changes (both
// stay `int`).
func (c *checker) adaptIntLiteralOperands(b *ast.Binary, lt, rt Type) (Type, Type) {
	if _, ok := unparen(b.Right).(*ast.IntLitExpr); ok && isIntegerType(lt) {
		c.info.Type[b.Right] = lt
		c.checkIntLitRange(lt, b.Right)
		rt = lt
	}
	if _, ok := unparen(b.Left).(*ast.IntLitExpr); ok && isIntegerType(rt) {
		c.info.Type[b.Left] = rt
		c.checkIntLitRange(rt, b.Left)
		lt = rt
	}
	return lt, rt
}

// numericResult checks both operands are the same numeric type and
// returns it; mismatches fire E0201.
func (c *checker) numericResult(lt, rt Type, b *ast.Binary) Type {
	// Dynamic / Any operands are governed by the boundary rules,
	// not by arithmetic typing — don't mislabel them here.
	if isDynamic(lt) || isDynamic(rt) || isAny(lt) || isAny(rt) {
		return &Unknown{}
	}
	if concrete(lt) && !isNumeric(lt) {
		c.report("E0201", "Type mismatch — `"+b.Op+"` expects a numeric type, got "+lt.String(), b.Left.NodeSpan())
		return &Unknown{}
	}
	if concrete(rt) && !isNumeric(rt) {
		c.report("E0201", "Type mismatch — `"+b.Op+"` expects a numeric type, got "+rt.String(), b.Right.NodeSpan())
		return &Unknown{}
	}
	c.expectSame(lt, rt, b, "operands of `"+b.Op+"`")
	if concrete(lt) {
		return lt
	}
	return rt
}

// expectSame fires E0201 when both operand types are concrete and
// unequal. The diagnostic points at the right operand.
//
// Named operands (class / sum types) are left alone: `==`/`!=` on
// class types routes to E0206 (`refEq`) and comparability of
// nominal types is the comparability PR's job, so reporting a
// generic E0201 here would mislabel the eventual diagnostic.
func (c *checker) expectSame(lt, rt Type, b *ast.Binary, what string) {
	if _, ok := lt.(*Named); ok {
		return
	}
	if _, ok := rt.(*Named); ok {
		return
	}
	// Dynamic / Any operand agreement is a boundary concern, not a
	// generic equality mismatch.
	if isDynamic(lt) || isDynamic(rt) || isAny(lt) || isAny(rt) {
		return
	}
	if concrete(lt) && concrete(rt) && !equal(lt, rt) {
		c.report("E0201", "Type mismatch — "+what+" require equal types, got "+lt.String()+" and "+rt.String(), b.Right.NodeSpan())
	}
}

func (c *checker) inferUnary(u *ast.Unary) Type {
	ot := c.inferExpr(u.Operand)
	switch u.Op {
	case "!":
		if concrete(ot) && !isBool(ot) {
			c.report("E0201", "Type mismatch — `!` expects bool, got "+ot.String(), u.Operand.NodeSpan())
		}
		return &Builtin{N: "bool"}
	case "-":
		if concrete(ot) && !isNumeric(ot) {
			c.report("E0201", "Type mismatch — unary `-` expects a numeric type, got "+ot.String(), u.Operand.NodeSpan())
			return &Unknown{}
		}
		return ot
	default:
		return &Unknown{}
	}
}

// inferMatch types a match expression. Arm bodies are inferred;
// the whole expression's type is the first concrete arm type
// (arms are required to agree, but arm-agreement diagnostics and
// exhaustiveness are Barrier D — here we only need a result type).
func (c *checker) inferMatch(m *ast.MatchExpr) Type {
	subjectType := c.inferExpr(m.Subject)
	c.checkExhaustive(m, subjectType)
	// A parser-desugared `catch` carries extra invariants a plain match does
	// not (Result subject + diverging handler); catch.go §checkCatch.
	c.checkCatch(m, subjectType)
	var result Type = &Unknown{}
	for _, arm := range m.Arms {
		c.checkNoFloatPat(arm.Pattern)
		if vp, ok := arm.Pattern.(*ast.VariantPat); ok {
			c.typeMatchPayload(subjectType, vp)
		}
		if tp, ok := arm.Pattern.(*ast.TuplePat); ok {
			c.typeMatchTuplePayload(subjectType, tp)
		}
		at := c.inferExpr(arm.Body)
		if isUnknown(result) && concrete(at) {
			if _, never := at.(*Never); !never {
				result = at
			}
		}
	}
	return result
}

// tryResultType types `try e` (T-Try-Result / T-Try-Option): it unwraps
// the inner Result<T, E> / Option<T> to T. For a Result, it fires E0403
// when the inner error type differs from the enclosing function's
// Result error type — v1 has no implicit error conversion (G11), so a
// `try` may only propagate the function's own error type. An inner
// shape other than Result/Option (e.g. an un-modelled binding) leaves
// the result Unknown. E0402 (try in a non-Result/Option function) is
// handled by the caller via curTryForbidden.
func (c *checker) tryResultType(inner Type, v *ast.TryExpr) Type {
	switch in := inner.(type) {
	case *Result:
		if out, ok := c.curReturn.(*Result); ok {
			if concrete(in.E) && concrete(out.E) && !equal(in.E, out.E) {
				c.report("E0403", "`try` propagates error type "+in.E.String()+" but the function's error type is "+out.E.String(), v.Span)
			}
		}
		return in.T
	case *Option:
		// A spawn body is a Result<unit, error> frame, so it cannot
		// propagate an Option — there is no error to feed the group, and
		// codegen's spawn bail (`return <tmp>.E`) has no `.E` to take
		// (E0408). Wrap the value in a Result or handle it with `match`.
		if c.curSpawnFrame {
			c.report("E0408", "`try` on an Option inside a spawn body — a spawn body is a `Result<unit, error>` frame, so only a Result may be propagated; wrap the value in a Result or use `match`", v.Span)
			return in.T
		}
		return in.T
	}
	return &Unknown{}
}

// typeMatchPayload assigns component types to the binding symbols of a
// variant pattern's sub-patterns, from the matched subject's sum type:
//
//	Result<T,E>: Ok(v)→v:T, Err(e)→e:E
//	Option<T>:   Some(v)→v:T   (None carries no payload)
//	user sum:    each sub-pattern → the variant field's declared type
//
// Mirrors checkForBinding/setForVar (body.go). WildcardPat subs and
// arity mismatches are skipped — arity / exhaustiveness is exhaust.go's
// concern, and an Unknown subject leaves the bindings Unknown.
func (c *checker) typeMatchPayload(subject Type, vp *ast.VariantPat) {
	if len(vp.QName) == 0 {
		return
	}
	name := vp.QName[len(vp.QName)-1]
	switch s := subject.(type) {
	case *Result:
		switch name {
		case "Ok":
			c.bindPayload(vp, 0, s.T)
		case "Err":
			c.bindPayload(vp, 0, s.E)
		}
	case *Option:
		if name == "Some" {
			c.bindPayload(vp, 0, s.T)
		}
	case *Named:
		td, ok := s.Decl.(*ast.TypeDecl)
		if !ok {
			return
		}
		body, ok := td.Body.(*ast.SumTypeBody)
		if !ok {
			return
		}
		for _, v := range body.Variants {
			if v.Name != name {
				continue
			}
			for i, fld := range v.Fields {
				c.bindPayload(vp, i, c.typeFromExpr(fld.DeclType))
			}
			return
		}
	}
}

// typeMatchTuplePayload types the bindings of a tuple-pattern arm
// (`match (s, e) { (Idle, InsertCoin(n)) => … }`) from the subject's
// tuple component types: each VariantPat component is typed through
// typeMatchPayload against its component type, and a fresh-ident
// component takes its whole component type. A component whose ident is
// itself a variant name (a nullary-variant ref like `Idle`) carries no
// payload. Arity / shape mismatches are exhaust.go's concern — a
// non-tuple or mis-arity subject leaves the bindings Unknown.
func (c *checker) typeMatchTuplePayload(subject Type, tp *ast.TuplePat) {
	tup, ok := subject.(*Tuple)
	if !ok || len(tup.Comps) != len(tp.Sub) {
		return
	}
	for j, sub := range tp.Sub {
		comp := tup.Comps[j]
		switch p := sub.(type) {
		case *ast.VariantPat:
			c.typeMatchPayload(comp, p)
		case *ast.IdentPat:
			if p.Name == "" || p.Name == "_" || isVariantName(comp, p.Name) {
				continue
			}
			if sym := c.info.Def[p]; sym != nil {
				sym.Type = comp
			}
		}
	}
}

// bindPayload types the i-th sub-pattern of a variant pattern against
// payload-field type t (skips nil types, out-of-range i).
func (c *checker) bindPayload(vp *ast.VariantPat, i int, t Type) {
	if t == nil || i >= len(vp.Sub) {
		return
	}
	c.bindPatternType(vp.Sub[i], t)
}

// bindPatternType assigns binding types under pattern p from the value
// type t it matches. It recurses through the binding-introducing
// pattern shapes — IdentPat binds the whole value; TuplePat distributes
// over a matching-arity tuple; RecordPat types each field via
// recordFieldType; a nested VariantPat re-enters typeMatchPayload.
// Literal / wildcard patterns bind nothing. Mirrors codegen's
// bindSubPattern (type-system.md §P-Record).
func (c *checker) bindPatternType(p ast.Pattern, t Type) {
	if t == nil {
		return
	}
	switch pat := p.(type) {
	case *ast.IdentPat:
		if pat.Name == "" || pat.Name == "_" {
			return
		}
		if sym := c.info.Def[pat]; sym != nil {
			sym.Type = t
		}
	case *ast.TuplePat:
		if isOpaqueHandle(t) {
			c.report("E1002", "Cannot destructure opaque foreign handle "+t.String(), pat.Span)
			return
		}
		if tup, ok := t.(*Tuple); ok && len(tup.Comps) == len(pat.Sub) {
			for i, sub := range pat.Sub {
				c.bindPatternType(sub, tup.Comps[i])
			}
		}
	case *ast.RecordPat:
		if isOpaqueHandle(t) {
			c.report("E1002", "Cannot destructure opaque foreign handle "+t.String(), pat.Span)
			return
		}
		for _, f := range pat.Fields {
			c.bindPatternType(f.Pattern, c.recordFieldType(t, f.Name))
		}
	case *ast.VariantPat:
		c.typeMatchPayload(t, pat)
	}
}

// checkNoFloatPat fires E0305 for a float-literal pattern anywhere in
// p (including nested in variant / tuple sub-patterns). Float equality
// on patterns is unsafe (type-system.md §patterns).
func (c *checker) checkNoFloatPat(p ast.Pattern) {
	switch v := p.(type) {
	case *ast.FloatLitPat:
		c.report("E0305", "Float-literal patterns are not allowed — use a wildcard with an `if` guard", v.Span)
	case *ast.VariantPat:
		for _, s := range v.Sub {
			c.checkNoFloatPat(s)
		}
	case *ast.TuplePat:
		for _, s := range v.Sub {
			c.checkNoFloatPat(s)
		}
	case *ast.RecordPat:
		for _, f := range v.Fields {
			c.checkNoFloatPat(f.Pattern)
		}
	case *ast.AltPat:
		for _, a := range v.Atoms {
			c.checkNoFloatPat(a)
		}
	}
}

// inferBlock checks a block's statements and yields its value: the
// trailing expression's type, or unit when there is none. It is the
// single implementation behind both block-as-expression typing and
// statement-position block checking (checkBlock).
func (c *checker) inferBlock(b *ast.Block) Type {
	for _, s := range b.Stmts {
		c.checkStmt(s)
	}
	if b.Trailing != nil {
		return c.inferExpr(b.Trailing)
	}
	return &Unit{}
}

// inferIfExpr types an `if`-expression. The result is the branch
// type when concrete; an `if` with no `else`, or with disagreeing /
// non-concrete branches, stays conservative-Unknown so no false
// positive fires (branch-agreement is a later Barrier-D concern).
func (c *checker) inferIfExpr(e *ast.IfExpr) Type {
	c.inferExpr(e.Cond)
	thenT := c.inferBlock(e.ThenBlock)
	switch x := e.Else.(type) {
	case *ast.IfExpr:
		c.inferIfExpr(x)
	case *ast.Block:
		c.inferBlock(x)
	}
	if concrete(thenT) {
		return thenT
	}
	return &Unknown{}
}

// checkReturn fires E0203 when a `return e` value disagrees with
// the enclosing function's declared return type.
func (c *checker) checkReturn(r *ast.ReturnExpr) {
	var got Type = &Unit{}
	if r.Value != nil {
		got = c.inferExpr(r.Value)
	}
	want := c.curReturn
	if want == nil {
		want = &Unit{}
	}
	// A bare `return` yields unit; only narrow / range-check when
	// there is an actual value expression.
	if r.Value == nil {
		if concrete(want) && !assignable(want, got) {
			c.report("E0203", "Wrong return type — function returns "+want.String()+", got "+got.String(), r.Span)
		}
		return
	}
	if !c.fits(want, r.Value, got) {
		c.report("E0203", "Wrong return type — function returns "+want.String()+", got "+got.String(), r.Span)
	}
	// Inferring an un-annotated closure return: record the value type so a
	// block-body closure that yields via `return` (no trailing expression)
	// types from its returns (T-Closure-Block). The returns must agree —
	// an un-annotated closure has no declared type to widen to, so an
	// inconsistent set is reported in `.aril` coordinates (E0203) rather
	// than left for Go's checker to reject the emitted signature (D10).
	if c.returnAcc != nil && concrete(got) {
		if len(*c.returnAcc) > 0 {
			first := (*c.returnAcc)[0]
			if !assignable(first, got) && !assignable(got, first) {
				c.report("E0203", "Inconsistent closure return types — an earlier branch returns "+first.String()+", this returns "+got.String()+"; annotate the return type `(…): T => …`", r.Span)
			}
		}
		*c.returnAcc = append(*c.returnAcc, got)
	}
}

// checkCallArity — E0202. Compares the call's positional argument
// count against the callee's declared parameter count when the
// callee is a user-declared func or class constructor reachable
// through Info. Methods, variadic and stdlib-binding calls are
// skipped (their arities are not modelled yet).
func (c *checker) checkCallArity(call *ast.Call) {
	id, ok := call.Callee.(*ast.Ident)
	if !ok {
		return
	}
	sym, ok := c.info.Symbol[id]
	if !ok || sym == nil {
		return
	}
	// A spread `...xs` contributes an unknown number of elements, so the
	// literal argument count can't be matched against the arity here —
	// validity is checked by checkSpreadTarget / checkArgTypes instead.
	for _, a := range call.Args {
		if _, ok := a.(*ast.SpreadArg); ok {
			return
		}
	}
	switch sym.Kind {
	case SymFunc:
		fn, ok := sym.Decl.(*ast.FuncDecl)
		if !ok {
			return
		}
		want := len(fn.Params)
		got := len(call.Args)
		if want > 0 && fn.Params[want-1].Variadic {
			// A variadic call needs at least the fixed parameters; the
			// trailing `...T` accepts zero or more (or one spread).
			nfixed := want - 1
			if got < nfixed {
				c.report("E0202",
					"Wrong arity in call to "+fn.Name+
						": expects at least "+strconv.Itoa(nfixed)+" "+pluralArgs(nfixed)+
						", got "+strconv.Itoa(got),
					call.Span)
			}
			return
		}
		if want != got {
			c.report("E0202",
				"Wrong arity in call to "+fn.Name+
					": expects "+strconv.Itoa(want)+" "+pluralArgs(want)+
					", got "+strconv.Itoa(got),
				call.Span)
		}
	case SymClass:
		cd, ok := sym.Decl.(*ast.ClassDecl)
		if !ok {
			return
		}
		want := len(cd.Fields)
		got := len(call.Args)
		if want != got {
			c.report("E0202",
				"Wrong arity in constructor "+cd.Name+
					": expects "+strconv.Itoa(want)+" field "+pluralArgs(want)+
					", got "+strconv.Itoa(got),
				call.Span)
		}
	}
}

// checkMethodArity fires E0202 when a method / value-handle-method call
// `recv.m(args)` supplies the wrong number of positional arguments. It
// mirrors the Ident-callee arity path (checkCallArity) for Field callees,
// whose arities the go/types backend would otherwise report against the
// generated Go method — leaking its Go name (a D10 gap). A spread `...xs`
// makes the literal count indeterminate, so arity is left to
// checkArgTypes. A bare type-parameter receiver has no modelled method
// signature (inferField returns Unknown, not a *Func), so it never
// reaches here.
func (c *checker) checkMethodArity(fn *Func, call *ast.Call, name string) {
	for _, a := range call.Args {
		if _, ok := a.(*ast.SpreadArg); ok {
			return
		}
	}
	want, got := len(fn.Params), len(call.Args)
	if fn.Variadic {
		if nfixed := want - 1; got < nfixed {
			c.report("E0202",
				"Wrong arity in call to "+name+": expects at least "+strconv.Itoa(nfixed)+" "+pluralArgs(nfixed)+", got "+strconv.Itoa(got),
				call.Span)
		}
		return
	}
	if want != got {
		c.report("E0202",
			"Wrong arity in call to "+name+": expects "+strconv.Itoa(want)+" "+pluralArgs(want)+", got "+strconv.Itoa(got),
			call.Span)
	}
}

// checkSpreadTarget enforces that a spread argument `...xs` only
// appears in a call to a variadic function/method (E0213). The parser
// already guarantees a spread is the final argument.
func (c *checker) checkSpreadTarget(call *ast.Call) {
	var spread *ast.SpreadArg
	for _, a := range call.Args {
		if sp, ok := a.(*ast.SpreadArg); ok {
			spread = sp
		}
	}
	if spread == nil {
		return
	}
	if fn := c.calleeFunc(call); fn == nil || !fn.Variadic {
		c.report("E0213", "Spread argument `...` requires a variadic parameter", spread.Span)
	}
}

// calleeFunc returns the resolved Func signature of a call's callee
// (an Ident-bound function/method or a typed Field method), or nil
// when the callee is not a Aril-typed function.
func (c *checker) calleeFunc(call *ast.Call) *Func {
	switch callee := call.Callee.(type) {
	case *ast.Ident:
		if sym := c.info.Symbol[callee]; sym != nil {
			if fn, ok := sym.Type.(*Func); ok {
				return fn
			}
		}
	case *ast.Field:
		if fn, ok := c.info.Type[callee].(*Func); ok {
			return fn
		}
	}
	return nil
}
