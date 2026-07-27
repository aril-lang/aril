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

// The same context-typing reaches a *method*-call argument (`recv.m((u) => …)`),
// not just a free-function call: seedClosureArgExpect resolves the method's Func
// via the inferred Field callee, so `u` types from the parameter and `u.name`
// resolves (CLOSURE-INFERENCE-2 P2). Without it, `u` is Unknown → E0214.
func TestClosureParamInferredFromMethodArg(t *testing.T) {
	src := `class User { let name: string }
class Suite {
  let n: int
  run(f: (User) => string): string { return f(User{ name: "x" }) }
}
func use(): string {
  let s = Suite{ n: 1 }
  return s.run((u) => u.name)
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (method-arg closure param inferred), got %v", codes)
	}
}

// A method-arg closure's result is inferred from the method parameter type too:
// a `(int) => unit` slot accepts an unannotated block-body closure without a
// `cannot infer closure result type` error (CLOSURE-INFERENCE-2 P2).
func TestClosureResultUnitInferredFromMethodArg(t *testing.T) {
	src := `func emit(n: int) {}
class Suite {
  let n: int
  it(desc: string, body: (int) => unit): int { body(n); return n }
}
func use(): int {
  let s = Suite{ n: 5 }
  return s.it("case", (x) => { emit(x) })
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (method-arg closure result inferred), got %v", codes)
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
