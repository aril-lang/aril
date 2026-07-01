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
