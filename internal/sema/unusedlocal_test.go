package sema

import "testing"

// An unused `let`/`var` local is E0221 (Go rejects it as "declared and not
// used"; we diagnose it in Aril terms). A referenced binding — including
// one used only through an assignment, a later let, a loop invariant, or a
// channel — is clean.
func TestUnusedLocalE0221(t *testing.T) {
	// A dead let → E0221.
	if codes := runCheck(t, `import fmt
func main() {
  let x = 5
  fmt.println("hi")
}
`); !hasCode(codes, "E0221") {
		t.Errorf("want E0221 for an unused let, got %v", codes)
	}

	// Referencing it silences E0221.
	if codes := runCheck(t, `import fmt
func main() {
  let x = 5
  fmt.println(x)
}
`); hasCode(codes, "E0221") {
		t.Errorf("a referenced let must not fire E0221, got %v", codes)
	}

	// `let _ = e` is the intended discard — no binding, no E0221.
	if codes := runCheck(t, `func main() {
  let _ = 5
}
`); hasCode(codes, "E0221") {
		t.Errorf("a `_` discard must not fire E0221, got %v", codes)
	}

	// A local referenced only from a loop invariant counts as used
	// (the predicate is a reference even though it is elided under
	// --contracts=off).
	if codes := runCheck(t, `func run(xs: []int): int {
  let cap = xs.len()
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}
contract run {
  loop scan {
    invariant acc <= cap
  }
}
func main() {}
`); hasCode(codes, "E0221") {
		t.Errorf("a local used only in a loop invariant must not fire E0221, got %v", codes)
	}

	// A channel binding is exempt (contracts / select / spawn use paths).
	if codes := runCheck(t, `func main() {
  let ch = makeChannel<int>(1)
  ch.send(1)
  ch.close()
}
`); hasCode(codes, "E0221") {
		t.Errorf("a channel binding must be exempt from E0221, got %v", codes)
	}
}

// A same-block re-declaration of a local is E0222; shadowing in a nested
// `{ … }` block is allowed (the inner binding is a different scope).
func TestRedeclareLocalE0222(t *testing.T) {
	if codes := runCheck(t, `import fmt
func main() {
  let x = 1
  let x = 2
  fmt.println(x)
}
`); !hasCode(codes, "E0222") {
		t.Errorf("want E0222 for a same-block re-let, got %v", codes)
	}

	// A same-block re-declaration reports E0222 only — not a redundant
	// E0221 on the shadowed first binding.
	if codes := runCheck(t, `import fmt
func main() {
  let x = 1
  let x = 2
  fmt.println(x)
}
`); hasCode(codes, "E0221") {
		t.Errorf("a re-let must not also fire E0221 on the shadowed binding, got %v", codes)
	}

	// Shadowing in a nested block is fine.
	if codes := runCheck(t, `import fmt
func main() {
  let x = 1
  {
    let x = 2
    fmt.println(x)
  }
  fmt.println(x)
}
`); hasCode(codes, "E0222") {
		t.Errorf("nested-block shadowing must not fire E0222, got %v", codes)
	}
}
