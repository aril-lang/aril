package sema

import "testing"

// A short closure with a block body that yields only via `return` (no
// trailing expression) types from its return statements, not as unit
// (T-Closure-Block). Before this, the diverging block typed unit and the
// closure was unusable in a value-returning position.

func TestClosureBlockReturnInfersType(t *testing.T) {
	// The closure return type flows into `apply`'s `(int) => int` param;
	// a clean check means it inferred `int`, not unit.
	src := `func apply(x: int, f: (int) => int): int { return f(x) }
func use(): int {
  return apply(5, (n: int) => { if n > 0 { return n } return 0 })
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (closure infers int return), got %v", codes)
	}
}

func TestClosureBlockReturnTypedInBinding(t *testing.T) {
	src := `func use(): int {
  let f = (a: int, b: int) => { if a > b { return a } return b }
  return f(3, 7)
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (block-body closure), got %v", codes)
	}
}

// A block body mixing an early `return` with a trailing value keeps the
// trailing value's type.
func TestClosureBlockMixedReturnAndTrailing(t *testing.T) {
	src := `func use(): int {
  let f = (x: int) => { if x < 0 { return 0 } x * 2 }
  return f(-5) + f(5)
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (mixed return + trailing), got %v", codes)
	}
}

// Inconsistent inferred returns in an un-annotated block-body closure are
// rejected in Aril coordinates (E0203), not left for Go's checker to
// reject the emitted signature (D10).
func TestClosureBlockInconsistentReturnsFiresE0203(t *testing.T) {
	src := `func use() {
  let f = (x: int) => { if x > 0 { return 1 } return "s" }
  let _ = f(3)
}`
	if codes := runCheck(t, src); !contains(codes, "E0203") {
		t.Errorf("expected E0203 (inconsistent closure returns), got %v", codes)
	}
}
