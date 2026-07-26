package sema

import "testing"

// A closure argument's unannotated parameter is typed from the callee's
// declared function-parameter type (seedClosureArgExpect), so a field access in
// the body resolves — without it `u` is Unknown and `u.name` cannot resolve.
func TestClosureParamInferredFromUserCallArg(t *testing.T) {
	src := `class User { let name: string }
func run(f: (User) => string): string { return f(User{ name: "x" }) }
func use(): string { return run((u) => u.name) }`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (closure param inferred from call context), got %v", codes)
	}
}

// A closure whose result is Result<unit, E> infers that result from the expected
// function type (bidirectional), so `return Ok(())` constrains E and a `try` in
// the body is admitted against the inferred Result frame.
func TestClosureResultUnitInferredFromExpected(t *testing.T) {
	src := `func want(cond: bool, msg: string): Result<unit, string> {
  if !cond { return Err(msg) }
  return Ok(())
}
func run(check: (int) => Result<unit, string>): bool {
  match check(0) {
    Ok(_) => { return true },
    Err(_) => { return false },
  }
}
func use(): bool {
  return run((n) => {
    try want(n > 0, "positive")
    return Ok(())
  })
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (closure Result<unit,E> result inferred), got %v", codes)
	}
}
