package sema

import (
	"testing"

	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

// runCheckHintDiags returns the full diagnostics so a test can assert both a
// hint's code and its non-blocking severity (D58).
func runCheckHintDiags(t *testing.T, src string) []*Diag {
	t.Helper()
	toks, lerr := lexer.LexFile(src, "test.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "test.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	_, diags := Check(f, "test.aril")
	return diags
}

func countHint(diags []*Diag, code string) int {
	n := 0
	for _, d := range diags {
		if d.Code == code && d.IsHint() {
			n++
		}
	}
	return n
}

func anyBlocking(diags []*Diag) bool {
	for _, d := range diags {
		if !d.IsHint() {
			return true
		}
	}
	return false
}

const hintPrelude = `func risky(): Result<unit, error> {
	return Ok(())
}
`

// TestDiscardedResultHintFires: a discarded Result in statement position
// (mid-block ExprStmt) and as a unit-body trailing expression both hint,
// and the hint is non-blocking (H0001, D58 / AUDIT-3 T16).
func TestDiscardedResultHintFires(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"mid-block statement", hintPrelude + `func main() {
	risky()
	let x = 1
	let _ = x
}`},
		{"unit-body trailing expression", hintPrelude + `func main() {
	risky()
}`},
		{"trailing in a unit helper", hintPrelude + `func helper() {
	risky()
}
func main() { helper() }`},
		{"if-statement branch trailing discard", hintPrelude + `func main() {
	if true {
		risky()
	}
}`},
		{"while-body trailing discard", hintPrelude + `func main() {
	while false {
		risky()
	}
}`},
		{"for-body trailing discard", hintPrelude + `func main() {
	for i in 0..1 {
		risky()
	}
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := runCheckHintDiags(t, tc.src)
			if got := countHint(diags, "H0001"); got != 1 {
				t.Fatalf("expected exactly one H0001 hint, got %d (%v)", got, diags)
			}
			if anyBlocking(diags) {
				t.Fatalf("H0001 must not be accompanied by a blocking error: %v", diags)
			}
		})
	}
}

// TestDiscardedResultHintSilent: forms that consume the Result — try, catch,
// match, a deliberate `let _ =`, and a trailing Result that IS the function's
// declared return value — must not hint (no false positive).
func TestDiscardedResultHintSilent(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"try consumes", hintPrelude + `func f(): Result<unit, error> {
	try risky()
	return Ok(())
}
func main() { let _ = f() }`},
		{"catch consumes", hintPrelude + `func main() {
	risky() catch e { return }
}`},
		{"let _ = deliberate discard", hintPrelude + `func main() {
	let _ = risky()
}`},
		{"trailing Result is the return value", hintPrelude + `func wrap(): Result<unit, error> {
	risky()
}
func main() { let _ = wrap() }`},
		{"assigned to a binding", hintPrelude + `func main() {
	let r = risky()
	let _ = r
}`},
		// A mixed if-*expression* used as a statement (one branch yields a
		// Result, the other unit) has an Unknown unified type, so the
		// "statically a Result" guard keeps it silent — an accepted
		// sound-over-complete miss (no false positive). A uniform-Result
		// if-expression-statement DOES hint via the ExprStmt arm.
		{"mixed if-expression-statement (known gap)", hintPrelude + `func main() {
	if false {
		let _ = 1
	} else {
		risky()
	}
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := runCheckHintDiags(t, tc.src)
			if got := countHint(diags, "H0001"); got != 0 {
				t.Fatalf("expected no H0001 hint, got %d (%v)", got, diags)
			}
		})
	}
}

const mapClassPrelude = `class Node {
	var v: int
	new(v: int) { this.v = v }
}
`

// TestBareMapClassReadHintFires: a bare `m[k]` read on a class-valued map hints
// (H0002, D58 / AUDIT-3 T13); the hint is non-blocking.
func TestBareMapClassReadHintFires(t *testing.T) {
	src := mapClassPrelude + `func main() {
	let m = Map<string, Node>{}
	m["a"] = Node(1)
	let n = m["a"]
	let _ = n
}`
	diags := runCheckHintDiags(t, src)
	if got := countHint(diags, "H0002"); got != 1 {
		t.Fatalf("expected exactly one H0002 hint, got %d (%v)", got, diags)
	}
	if anyBlocking(diags) {
		t.Fatalf("H0002 must not be accompanied by a blocking error: %v", diags)
	}
}

// TestBareMapClassReadHintSilent: a store target, a `.get(k)`, a scalar-valued
// map read, and a container-valued map read (defaulted to empty by R1) do not
// hint.
func TestBareMapClassReadHintSilent(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"store target", mapClassPrelude + `func main() {
	let m = Map<string, Node>{}
	m["a"] = Node(1)
}`},
		{"safe .get form", mapClassPrelude + `func main() {
	let m = Map<string, Node>{}
	let n = m.get("a")
	let _ = n
}`},
		{"scalar value", `func main() {
	let m = Map<string, int>{}
	m["a"] = 1
	let n = m["a"]
	let _ = n
}`},
		{"container value (R1 empty default)", `func main() {
	let m = Map<string, List<int>>{}
	let xs = m["a"]
	let _ = xs
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := runCheckHintDiags(t, tc.src)
			if got := countHint(diags, "H0002"); got != 0 {
				t.Fatalf("expected no H0002 hint, got %d (%v)", got, diags)
			}
		})
	}
}

// TestHintSeverityRenders: the hint renders with a `hint[...]` prefix, not
// `error[...]`, so the CLI can tell it apart (D58).
func TestHintSeverityRenders(t *testing.T) {
	d := &Diag{File: "m.aril", Code: "H0001", Message: "x", Line: 3, Col: 5, Severity: SeverityHint}
	got := d.Error()
	want := "m.aril:3:5: hint[H0001]: x"
	if got != want {
		t.Fatalf("hint render mismatch:\n got %q\nwant %q", got, want)
	}
	// A default (zero-severity) diagnostic still renders as an error.
	e := &Diag{File: "m.aril", Code: "E0201", Message: "y", Line: 1, Col: 1}
	if e.Error() != "m.aril:1:1: error[E0201]: y" {
		t.Fatalf("error render regressed: %q", e.Error())
	}
}
