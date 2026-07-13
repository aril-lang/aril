package sema

import (
	"strings"
	"testing"
)

// E0214 — an unknown member on a *concrete* Named receiver is reported in
// Aril coordinates rather than leaking a go/types "has no field or method"
// error against generated Go (D10). Covered across every concrete receiver
// kind: user class, record, interface (incl. the `extends` chain), and a
// bound stdlib value-handle (D37).

func TestUnknownClassMemberFiresE0214(t *testing.T) {
	src := `class Bar { let x: int  foo(): int { return x } }
func use(b: Bar): int { return b.nope() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown class member), got %v", codes)
	}
}

func TestUnknownRecordMemberFiresE0214(t *testing.T) {
	src := `type Point = { x: int, y: int }
func use(p: Point): int { return p.z }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown record member), got %v", codes)
	}
}

func TestUnknownInterfaceMemberFiresE0214(t *testing.T) {
	src := `interface Foo { foo(): int }
interface Composite extends Foo { }
func use(c: Composite): int { return c.missing() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown interface member), got %v", codes)
	}
}

func TestUnknownHandleMemberFiresE0214(t *testing.T) {
	src := `import regexp
func use(): bool { let re = regexp.mustCompile("x")  return re.noSuchMethod("a") }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown handle member), got %v", codes)
	}
}

// E0214 also covers a builtin-generic receiver (Map/Set/Stack/[]T/Option/
// Result/channel) — its method set is fully known (containerMethodType /
// channelMethodType), so an unresolved method is a real unknown-member error,
// not the go/types leak `type arilrt.Option[int] has no field or method …`.

func TestUnknownOptionMemberFiresE0214(t *testing.T) {
	// `map` is now a real Option method (inferMap), so use a name that is
	// genuinely absent from the method set.
	src := `func use(o: Option<int>) { let _ = o.frobnicate() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown Option member), got %v", codes)
	}
}

func TestUnknownMapMemberFiresE0214(t *testing.T) {
	src := `func use(m: Map<int, int>) { let _ = m.nope() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown Map member), got %v", codes)
	}
}

func TestUnknownSliceMemberFiresE0214(t *testing.T) {
	src := `func use(xs: []int) { let _ = xs.nope() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown slice member), got %v", codes)
	}
}

func TestUnknownChannelMemberFiresE0214(t *testing.T) {
	src := `func use(ch: Channel<int>) { let _ = ch.nope() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown channel member), got %v", codes)
	}
}

// A handle's *field* axis (HTTP-CLIENT epoch): a bound field on http.Response
// resolves (no E0214), while an unknown field on the same handle still fires
// E0214 — the field lookup slots in before the unknown-member path, and the
// header field chains to a further method call on the http.Header it yields.
func TestUnknownHandleFieldFiresE0214(t *testing.T) {
	src := `import http
func use(resp: http.Response): int { return resp.noSuchField }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown handle field), got %v", codes)
	}
}

func TestKnownHandleFieldNoE0214(t *testing.T) {
	src := `import http
func use(resp: http.Response): string {
  let code = resp.statusCode
  return resp.header.get("Content-Type")
}`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("bound http.Response fields + http.Header.get should not fire E0214, got %v", codes)
	}
}

// A deferred-but-documented Map method (entries, unimplemented in v1) is an
// unknown member today — the diagnostic agrees with the spec's `deferred` mark.
func TestDeferredMapEntriesFiresE0214(t *testing.T) {
	src := `func use(m: Map<int, int>) { let _ = m.entries() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (deferred Map.entries), got %v", codes)
	}
}

// E0214 also covers a primitive receiver with a closed method set — numeric
// types and bool have none, string has len/bytes/runes, error has error(). An
// unknown method on one is a real miss, not the go/types leak `type int has no
// field or method …`. Any/Dynamic (separate types, escape hatches) stay silent.

func TestUnknownIntMemberFiresE0214(t *testing.T) {
	src := `func use(n: int): int { return n.nope() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown int member), got %v", codes)
	}
}

func TestUnknownBoolMemberFiresE0214(t *testing.T) {
	src := `func use(b: bool): bool { return b.flip() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown bool member), got %v", codes)
	}
}

func TestUnknownErrorMemberFiresE0214(t *testing.T) {
	src := `func use(e: error): string { return e.stack() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown error member), got %v", codes)
	}
}

// The `error` interface's real method must resolve (typed as string), not fire
// E0214 — the false-positive guard for the one primitive with a method.
func TestErrorDotErrorNoE0214(t *testing.T) {
	src := `func use(e: error): string { return e.error() }`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("valid e.error() must not fire E0214, got %v", codes)
	}
}

func TestErrorDotErrorTypesString(t *testing.T) {
	src := `func use(e: error) {
  let m = e.error()
}`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "m"); got == nil || got.String() != "string" {
		t.Errorf("e.error() = %v; want string", got)
	}
}

// An Any-typed receiver (the binding-boundary escape type) has no known member
// set, so a method on it must NOT fire E0214 — sound over complete.
func TestAnyMemberNoE0214(t *testing.T) {
	src := `extern func raw(): Any
func use() { let _ = raw().whatever() }`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("Any-typed receiver member must not fire E0214, got %v", codes)
	}
}

// The known builtin-generic methods must NOT fire E0214 (they resolve through
// containerMethodType/channelMethodType) — the false-positive guard.
func TestKnownContainerMembersNoE0214(t *testing.T) {
	src := `func use(o: Option<int>, r: Result<int, string>, m: Map<int, int>): int {
  let a = o.unwrapOr(0)
  let b = r.unwrapOr(0)
  let c = m.len()
  return a + b + c
}`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("known container members must not fire E0214, got %v", codes)
	}
}

// `mapErr` is a real Result method (typed in inferCall, not containerMethodType),
// so a correct 1-arg call must not be misreported as an unknown member.
func TestResultMapErrNoE0214(t *testing.T) {
	src := `func use(r: Result<int, error>): Result<int, string> {
  return r.mapErr((e) => "wrapped")
}`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("valid mapErr must not fire E0214, got %v", codes)
	}
}

// E0202 also covers `mapErr` with the wrong arity — a real Result method whose
// dynamic-E2 result keeps it out of containerMethodType, so its arity is checked
// explicitly (0-/2-arg calls otherwise leak the MapErr helper's go/types error).

func TestMapErrZeroArgFiresE0202(t *testing.T) {
	src := `func use(r: Result<int, error>) { let _ = r.mapErr() }`
	if codes := runCheck(t, src); !contains(codes, "E0202") {
		t.Errorf("expected E0202 (mapErr 0-arg), got %v", codes)
	}
}

func TestMapErrTwoArgFiresE0202(t *testing.T) {
	src := `func use(r: Result<int, error>) { let _ = r.mapErr((e) => e, 9) }`
	if codes := runCheck(t, src); !contains(codes, "E0202") {
		t.Errorf("expected E0202 (mapErr 2-arg), got %v", codes)
	}
}

func TestMapErrOneArgNoE0202(t *testing.T) {
	src := `func use(r: Result<int, error>): Result<int, string> {
  return r.mapErr((e) => "x")
}`
	if codes := runCheck(t, src); contains(codes, "E0202") {
		t.Errorf("correct 1-arg mapErr must not fire E0202, got %v", codes)
	}
}

// A method inherited through the `extends` chain is a *known* member, so
// access is typed and E0214 does NOT fire — the guard against a false
// positive that the interfaces corpus example first surfaced.
func TestInheritedInterfaceMemberNoE0214(t *testing.T) {
	src := `interface Foo { foo(): int }
interface Composite extends Foo { }
func use(c: Composite): int { return c.foo() }`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("inherited interface member must not fire E0214, got %v", codes)
	}
}

// A bare type parameter has no known member set (its Decl is nil and it is
// not a handle), so a member access on it is NOT a user error — the guard
// that keeps generic code silent.
func TestTypeParamMemberNoE0214(t *testing.T) {
	src := `func use<T>(x: T) { let _ = x.anything() }`
	if codes := runCheck(t, src); contains(codes, "E0214") {
		t.Errorf("type-parameter member access must not fire E0214, got %v", codes)
	}
}

// E0202 — a method / value-handle-method call with the wrong positional
// arity is reported for the Field callee (previously skipped, leaking a Go
// "too many arguments" against the generated method — a D10 gap).

func TestMethodArityFiresE0202(t *testing.T) {
	src := `class Bar { let x: int  foo(): int { return x } }
func use(b: Bar): int { return b.foo(9) }`
	if codes := runCheck(t, src); !contains(codes, "E0202") {
		t.Errorf("expected E0202 (method arity), got %v", codes)
	}
}

func TestInterfaceMethodArityFiresE0202(t *testing.T) {
	src := `interface Foo { foo(): int }
func use(f: Foo): int { return f.foo(1) }`
	if codes := runCheck(t, src); !contains(codes, "E0202") {
		t.Errorf("expected E0202 (interface method arity), got %v", codes)
	}
}

func TestCorrectMethodArityNoE0202(t *testing.T) {
	src := `class Bar { let x: int  foo(): int { return x } }
func use(b: Bar): int { return b.foo() }`
	if codes := runCheck(t, src); contains(codes, "E0202") {
		t.Errorf("correct method arity must not fire E0202, got %v", codes)
	}
}

// E0401 — comparing an Option / Result with `==` is rejected (they are
// inspected with `match`, not compared). The tailored fix-hint is checked
// by the corpus negative case; here we lock that the code fires.

func TestOptionEqualityFiresE0401(t *testing.T) {
	src := `func find(): Option<int> { return None }
func use(): bool { return find() == None }`
	if codes := runCheck(t, src); !contains(codes, "E0401") {
		t.Errorf("expected E0401 (Option == None), got %v", codes)
	}
}

func TestResultEqualityFiresE0401(t *testing.T) {
	src := `func go(): Result<int, error> { return Ok(1) }
func use(): bool { return go() == Ok(1) }`
	if codes := runCheck(t, src); !contains(codes, "E0401") {
		t.Errorf("expected E0401 (Result equality), got %v", codes)
	}
}

// `[]T` has no `push` (D55) — a slice is a value view, not a growable
// container. A `xs.push(e)` now misses the slice method set → a tailored
// E0214 teaching the List<T> replacement (E0215 was retired with the method:
// it existed only to catch a discarded functional append, which no longer
// exists). Grow-in-place lives on List<T>.
func TestSlicePushFiresTailoredE0214(t *testing.T) {
	src := "import fmt\nfunc use() {\n  var xs = [1, 2, 3]\n  xs.push(4)\n  fmt.println(xs.len())\n}"
	codes := runCheck(t, src)
	if !contains(codes, "E0214") {
		t.Errorf("expected E0214 (slice has no push), got %v", codes)
	}
}

// The tailored message names List<T> (the migration target), not the generic
// "no member" — the audit's teach-the-trap-loudly move.
func TestSlicePushMessageNamesList(t *testing.T) {
	src := "func use() {\n  var xs = [1, 2, 3]\n  xs.push(4)\n}"
	msgs := runCheckMsgs(t, src)
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "no `push`") && strings.Contains(m, "List<T>") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a slice-push message naming List<T>, got %v", msgs)
	}
}

// The pure slice accessors survive — len() and copy() still resolve cleanly.
func TestSlicePureAccessorsClean(t *testing.T) {
	src := "func use(): int {\n  var xs = [1, 2, 3]\n  let ys = xs.copy()\n  return xs.len() + ys.len()\n}"
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (slice len/copy survive), got %v", codes)
	}
}

// Stack `push` mutates in place and is unaffected — a bare statement is
// legitimate and resolves (the D55 removal is slice-only).
func TestStackPushStillResolves(t *testing.T) {
	src := "func use() {\n  var st = Stack<int>{}\n  st.push(4)\n}"
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("Stack push must still resolve cleanly, got %v", codes)
	}
}

// E0216 — a `let` is an immutable single-assignment binding; reassigning
// it is rejected (a mutable local is spelled `var`).

func TestLetReassignFiresE0216(t *testing.T) {
	src := "func use() {\n  let i = 2\n  i = i + 1\n}"
	if codes := runCheck(t, src); !contains(codes, "E0216") {
		t.Errorf("expected E0216 (let reassignment), got %v", codes)
	}
}

func TestVarReassignNoE0216(t *testing.T) {
	src := "func use() {\n  var i = 2\n  i = i + 1\n}"
	if codes := runCheck(t, src); contains(codes, "E0216") {
		t.Errorf("var reassignment must not fire E0216, got %v", codes)
	}
}

// Mutating *through* a `let` binding (a field or element write) is not a
// rebind of the binding, so E0216 must not fire.
func TestLetElementWriteNoE0216(t *testing.T) {
	src := "func use() {\n  let xs = [1, 2, 3]\n  xs[0] = 9\n}"
	if codes := runCheck(t, src); contains(codes, "E0216") {
		t.Errorf("element write through a let must not fire E0216, got %v", codes)
	}
}
