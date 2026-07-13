package sema

import "testing"

// Container-method return typing (T-Container-Method). Before this, a
// `Map`/`Set`/`Stack`/`[]T` method call typed Unknown, so a match
// payload over `m.get(k)` was untyped and downstream tuple/constructor
// inference failed.

func TestMapGetPayloadTypesValue(t *testing.T) {
	src := `func f(m: Map<int, string>, k: int) {
  match m.get(k) {
    Some(v) => { let a = v },
    None    => {},
  }
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "v"); got == nil || got.String() != "string" {
		t.Errorf("Some(v) payload = %v; want string", got)
	}
}

func TestStackPopPayloadTypesElem(t *testing.T) {
	src := `func f(s: Stack<int>) {
  match s.pop() {
    Ok(v)  => { let a = v },
    Err(_) => {},
  }
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "v"); got == nil || got.String() != "int" {
		t.Errorf("Ok(v) payload = %v; want int", got)
	}
}

func TestSetHasIsBool(t *testing.T) {
	// `s.has(e)` typed bool: a non-bool `if` head would otherwise be
	// silently Unknown; here the method result feeds a clean program.
	src := `func f(s: Set<int>): int {
  if s.has(1) { return s.len() }
  return 0
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (has:bool, len:int), got %v", codes)
	}
}

func TestMapKeysIsSliceOfKey(t *testing.T) {
	src := `func f(m: Map<int, string>): int {
  var total = 0
  for k in m.keys() { total = total + k }
  return total
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (keys:[]int), got %v", codes)
	}
}

// `[]T` lost `.push` (D55) — a slice is a value view. `xs.push(1)` now misses
// the slice method set → E0214 (the pure accessors len/copy survive; grow-in-
// place moved to List<T>). Covered in depth in membermethod_test.go.
func TestSlicePushNoLongerResolves(t *testing.T) {
	src := `func f(xs: []int) { xs.push(1) }
`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (slice has no push), got %v", codes)
	}
}

// Option/Result query + defaulting methods (builtins.md §Option/§Result
// methods): the predicates type bool, unwrapOr types the payload/Ok type.
func TestOptionMethodsType(t *testing.T) {
	src := `func f(o: Option<int>): int {
  if o.isSome() { return o.unwrapOr(0) }
  if o.isNone() { return -1 }
  return o.unwrapOr(0)
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (isSome/isNone:bool, unwrapOr:int), got %v", codes)
	}
}

func TestOptionUnwrapOrTypesPayload(t *testing.T) {
	src := `func f(o: Option<string>) {
  let s = o.unwrapOr("x")
  let n = s.len()
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "s"); got == nil || got.String() != "string" {
		t.Errorf("unwrapOr payload = %v; want string", got)
	}
}

func TestResultMethodsType(t *testing.T) {
	src := `func f(r: Result<int, string>): int {
  if r.isOk() { return r.unwrapOr(0) }
  if r.isErr() { return -1 }
  return r.unwrapOr(0)
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (isOk/isErr:bool, unwrapOr:int), got %v", codes)
	}
}

// unwrapOr defaults to the Ok payload type — the fallback must be that
// type, not the Err type. A mismatched fallback is an arg-type error.
func TestResultUnwrapOrTypesOkPayload(t *testing.T) {
	src := `func f(r: Result<int, string>) {
  let v = r.unwrapOr(0)
  let w = v + 1
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "v"); got == nil || got.String() != "int" {
		t.Errorf("unwrapOr payload = %v; want int", got)
	}
}

// String methods (builtins.md §Lowering pointers): `.len()` types int, `.bytes()`
// []byte, `.runes()` []rune — the view/length helpers codegen lowers via
// `len(s)`/`[]byte(s)`/`[]rune(s)`. Before this a string method call typed
// Unknown, which then broke closure-return inference over it.
func TestStringLenTypesInt(t *testing.T) {
	src := `func f(s: string) {
  let n = s.len()
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "n"); got == nil || got.String() != "int" {
		t.Errorf("s.len() = %v; want int", got)
	}
}

func TestStringBytesRunesTypeSlices(t *testing.T) {
	src := `func f(s: string) {
  let b = s.bytes()
  let r = s.runes()
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "b"); got == nil || got.String() != "[]byte" {
		t.Errorf("s.bytes() = %v; want []byte", got)
	}
	if got := defTypeByName(info, "r"); got == nil || got.String() != "[]rune" {
		t.Errorf("s.runes() = %v; want []rune", got)
	}
}

// The typing fix back-propagates a closure's result type when its body is a
// string method call — `(s: string) => s.len()` now infers a `func(string): int`
// instead of leaving the return Unknown (which had failed codegen: "cannot infer
// closure result type", and blocked sort.sortedBy key-with-method-call). Assert
// the *concrete* Func type, not `len(codes)==0`: an Unknown return unifies with
// anything, so a no-diagnostic oracle passes falsely here (dev-insights §6).
func TestClosureOverStringLenInfersInt(t *testing.T) {
	src := `func f() {
  let key = (s: string) => s.len()
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "key"); got == nil || got.String() != "func(string): int" {
		t.Errorf("closure over s.len() = %v; want func(string): int", got)
	}
}

// mapErr transforms the Err payload E→E2 (Ok's T preserved), the new E2 read
// from the handler's return (T-Result-MapErr): `(e: string) => ParseError`
// yields Result<int, ParseError>, so the whole call's type carries the new E.
func TestResultMapErrTypesNewErrorPayload(t *testing.T) {
	src := `type ParseError = { detail: string }
func f(r: Result<int, string>) {
  let x = r.mapErr((e) => ParseError{ detail: e })
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "x"); got == nil || got.String() != "Result<int, ParseError>" {
		t.Errorf("mapErr result = %v; want Result<int, ParseError>", got)
	}
}

// mapErr pre-types an unannotated handler param as the receiver's E, so its
// body checks against the real error type (here `e` is a string, concatenated)
// and the whole call unifies with the declared Result<T, E2> return.
func TestResultMapErrHandlerParamTypedAsError(t *testing.T) {
	src := `func f(r: Result<int, string>): Result<int, string> {
  return r.mapErr((e) => e + "!")
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (mapErr handler param string), got %v", codes)
	}
}

// Result.map transforms the Ok payload T→U (Err's E preserved), the new U read
// from the mapper's return: `(x: int) => string` on Result<int, string> yields
// Result<string, string>.
func TestResultMapTypesNewOkPayload(t *testing.T) {
	src := `func f(r: Result<int, string>) {
  let x = r.map((v) => "n=" + v.toString())
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "x"); got == nil || got.String() != "Result<string, string>" {
		t.Errorf("map result = %v; want Result<string, string>", got)
	}
}

// Option.map transforms the Some payload T→U, staying an Option: `(x: int) =>
// bool` on Option<int> yields Option<bool>. The mapper's unannotated param is
// pre-typed as the receiver's payload (T-Option-Map).
func TestOptionMapTypesNewPayload(t *testing.T) {
	src := `func f(o: Option<int>) {
  let x = o.map((v) => v > 0)
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "x"); got == nil || got.String() != "Option<bool>" {
		t.Errorf("map result = %v; want Option<bool>", got)
	}
}

// map with the wrong arity on an Option/Result receiver is a real method miss
// (exactly one mapper arg) → E0202, not a go/types leak from the helper.
func TestMapWrongArityFiresE0202(t *testing.T) {
	src := `func f(o: Option<int>) { let _ = o.map() }`
	if codes := runCheck(t, src); !contains(codes, "E0202") {
		t.Errorf("expected E0202 (map arity), got %v", codes)
	}
}
