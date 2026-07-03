package sema

import (
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

// A pattern binding referenced by its arm body is marked Symbol.Used; one
// the body ignores is not. Codegen reads this flag to decide the `_ = b`
// bind-and-ignore guard (lowering-go.md §MatchIR). Shadowing is respected:
// the flag lives on the innermost binding a use resolves to.
func TestPatternBindingUsedFlag(t *testing.T) {
	src := `import fmt
func main() {
  let r: Result<int, string> = Err("boom")
  match r {
    Ok(v) => fmt.println(v),
    Err(e) => fmt.println("no")
  }
  let xs = [1, 2, 3]
  for (i, x) in xs { fmt.println(i) }
}`
	toks, lerr := lexer.LexFile(src, "test.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "test.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	info, diags := Check(f, "test.aril")
	if len(diags) != 0 {
		t.Fatalf("expected clean, got %d diagnostics", len(diags))
	}

	used := map[string]bool{}
	for node, sym := range info.Def {
		if _, ok := node.(*ast.IdentPat); ok {
			used[sym.Name] = sym.Used
		}
	}
	// v (match Ok payload) and i (for index) are referenced; e (match Err
	// payload) and x (for value) are bound-and-ignored.
	for name, want := range map[string]bool{"v": true, "i": true, "e": false, "x": false} {
		got, ok := used[name]
		if !ok {
			t.Errorf("no pattern binding %q recorded in info.Def", name)
			continue
		}
		if got != want {
			t.Errorf("binding %q: Used = %v, want %v", name, got, want)
		}
	}
}
