package sema

import (
	"testing"

	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

func checkContract(t *testing.T, src string) (*Info, []string) {
	t.Helper()
	toks, lerr := lexer.LexFile(src, "test.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "test.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	info, diags := Check(f, "test.aril")
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return info, codes
}

// TestLoopInvariantChecked: a well-formed loop invariant is bound + typed and
// recorded on Info.LoopInvariants, with no diagnostics.
func TestLoopInvariantChecked(t *testing.T) {
	info, codes := checkContract(t, `func run(xs: []int): int {
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}

contract run {
  loop scan {
    invariant acc >= 0
  }
}
`)
	if len(codes) != 0 {
		t.Fatalf("clean loop invariant produced diags: %v", codes)
	}
	if len(info.LoopInvariants) != 1 {
		t.Fatalf("expected 1 loop with invariants recorded, got %d", len(info.LoopInvariants))
	}
	for _, preds := range info.LoopInvariants {
		if len(preds) != 1 {
			t.Errorf("expected 1 invariant predicate, got %d", len(preds))
		}
	}
}

// TestLoopInvariantBadLabel: a loop section naming a non-existent loop label
// is E1101.
func TestLoopInvariantBadLabel(t *testing.T) {
	_, codes := checkContract(t, `func run(xs: []int): int {
  for i in 0..xs.len() loop scan {}
  return 0
}

contract run {
  loop nope {
    invariant true
  }
}
`)
	if !hasCode(codes, "E1101") {
		t.Errorf("want E1101 for an unmatched loop label, got %v", codes)
	}
}

// TestLoopInvariantNonBool: a non-bool invariant predicate is E1102.
func TestLoopInvariantNonBool(t *testing.T) {
	_, codes := checkContract(t, `func run(xs: []int): int {
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}

contract run {
  loop scan {
    invariant acc
  }
}
`)
	if !hasCode(codes, "E1102") {
		t.Errorf("want E1102 for a non-bool invariant, got %v", codes)
	}
}

// TestContractUnknownTarget: a contract attaching to no declaration is E1101.
func TestContractUnknownTarget(t *testing.T) {
	_, codes := checkContract(t, `func main() {}

contract nope {
  loop scan {
    invariant true
  }
}
`)
	if !hasCode(codes, "E1101") {
		t.Errorf("want E1101 for an unknown contract target, got %v", codes)
	}
}

// TestLoopInvariantNestedInIf: a labelled loop nested in an `if` is matched
// by the resolve walk (no false E1101) — guards the match/scope/select
// robustness fix (the walk visits every loop, not a partial collector).
func TestLoopInvariantNestedInIf(t *testing.T) {
	_, codes := checkContract(t, `func run(xs: []int): int {
  var acc = 0
  if xs.len() > 0 {
    for i in 0..xs.len() loop scan {
      acc = acc + xs[i]
    }
  }
  return acc
}

contract run {
  loop scan {
    invariant acc >= 0
  }
}
`)
	if len(codes) != 0 {
		t.Errorf("nested labelled loop should match (no E1101); got %v", codes)
	}
}

// TestEnsuresChecked: requires/ensures/entry resolve+type-check and land on
// Info.FuncContracts; `result` and entry names resolve; a non-bool ensures is
// E1102.
func TestEnsuresChecked(t *testing.T) {
	info, codes := checkContract(t, `func dbl(x: int): int { return x + x }

contract dbl {
  requires x >= 0
  entry { let x0 = x }
  ensures result == x0 + x0
}
`)
	if len(codes) != 0 {
		t.Fatalf("clean contract produced diags: %v", codes)
	}
	fcs := info.FuncContracts
	if len(fcs) != 1 {
		t.Fatalf("expected 1 FuncContract recorded, got %d", len(fcs))
	}
	for _, fc := range fcs {
		if len(fc.Requires) != 1 || len(fc.Ensures) != 1 || len(fc.Entries) != 1 {
			t.Errorf("fc = %+v, want 1 requires/ensures/entry each", fc)
		}
	}
}

// TestEnsuresNonBool: a non-bool ensures predicate is E1102.
func TestEnsuresNonBool(t *testing.T) {
	_, codes := checkContract(t, `func f(x: int): int { return x }

contract f {
  ensures result
}
`)
	if !hasCode(codes, "E1102") {
		t.Errorf("want E1102 for a non-bool ensures, got %v", codes)
	}
}
