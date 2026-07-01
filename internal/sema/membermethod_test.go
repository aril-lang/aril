package sema

import "testing"

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

// E0215 — a slice `push` whose result is discarded is a silent no-op
// (append semantics: `push` returns a new slice, never mutates in place).
// Go would leak `append(xs, e) … is not used` against the lowered form.

func TestDiscardedSlicePushFiresE0215(t *testing.T) {
	// Statement position (a bare push followed by more code) — the
	// unambiguously-discarded case the check targets.
	src := "import fmt\nfunc use() {\n  var xs = [1, 2, 3]\n  xs.push(4)\n  fmt.println(xs.len())\n}"
	if codes := runCheck(t, src); !contains(codes, "E0215") {
		t.Errorf("expected E0215 (discarded slice push), got %v", codes)
	}
}

func TestAssignedSlicePushNoE0215(t *testing.T) {
	src := "func use() {\n  var xs = [1, 2, 3]\n  xs = xs.push(4)\n}"
	if codes := runCheck(t, src); contains(codes, "E0215") {
		t.Errorf("assigned-back slice push must not fire E0215, got %v", codes)
	}
}

// Stack `push` mutates in place, so a bare statement is legitimate — the
// no-op diagnostic must not fire on it (gates on a slice receiver).
func TestDiscardedStackPushNoE0215(t *testing.T) {
	src := "func use() {\n  var st = Stack<int>{}\n  st.push(4)\n}"
	if codes := runCheck(t, src); contains(codes, "E0215") {
		t.Errorf("Stack push statement must not fire E0215, got %v", codes)
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
