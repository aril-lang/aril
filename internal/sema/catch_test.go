package sema

import "testing"

// The `expr catch e { … }` control-flow form (catch.go). It desugars to a
// FromCatch match; these lock the two catch-specific invariants and the Ok
// value type.

// A well-formed catch on a Result with a diverging handler is clean, and the
// expression types as the Ok payload T (so downstream use sees the real type).
func TestCatchTypesOkPayloadAndIsClean(t *testing.T) {
	src := `func f(r: Result<int, string>): int {
  let n = r catch e { return 0 }
  return n + 1
}
`
	if codes := codesOf(t, src); len(codes) != 0 {
		t.Fatalf("expected clean catch, got %v", codes)
	}
	info := checkInfo(t, src)
	if got := defTypeByName(info, "n"); got == nil || got.String() != "int" {
		t.Errorf("catch value = %v; want int (the Ok payload)", got)
	}
}

// E0409 — a catch handler that falls through with a value (does not diverge)
// is rejected; recovering-and-continuing is unwrapOr's job, not catch's.
func TestCatchNonDivergingHandlerE0409(t *testing.T) {
	src := `import fmt
func f(r: Result<int, string>): int {
  let n = r catch e { fmt.println(e) }
  return n
}
`
	if codes := codesOf(t, src); !hasCode(codes, "E0409") {
		t.Errorf("expected E0409 (non-diverging catch handler), got %v", codes)
	}
}

// A handler that diverges via os.exit (not return) also satisfies E0409.
func TestCatchDivergesViaExit(t *testing.T) {
	src := `import os
func f(r: Result<int, string>): int {
  let n = r catch e { os.exit(1) }
  return n
}
`
	if codes := codesOf(t, src); hasCode(codes, "E0409") {
		t.Errorf("os.exit handler should satisfy the divergence rule, got %v", codes)
	}
}

// E0410 — catch requires a Result subject; a non-Result has no Err to bind.
func TestCatchNonResultSubjectE0410(t *testing.T) {
	src := `func f(x: int): int {
  let n = x catch e { return 0 }
  return n
}
`
	if codes := codesOf(t, src); !hasCode(codes, "E0410") {
		t.Errorf("expected E0410 (catch on non-Result), got %v", codes)
	}
}
