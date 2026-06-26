package parser

import (
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

func parseString(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		t.Fatalf("lex error: %v", lerr)
	}
	f, perr := Parse(toks)
	if perr != nil {
		t.Fatalf("parse error: %v", perr)
	}
	return f
}

func TestHello(t *testing.T) {
	src := `import fmt

func main() {
  fmt.println("Aril is rising.")
}
`
	f := parseString(t, src)
	if len(f.Imports) != 1 || f.Imports[0].Path != "fmt" {
		t.Errorf("expected one import `fmt`; got %+v", f.Imports)
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected one decl; got %d", len(f.Decls))
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl[0] not FuncDecl: %T", f.Decls[0])
	}
	if fn.Name != "main" {
		t.Errorf("func name = %q; want \"main\"", fn.Name)
	}
	// The lone `fmt.println(...)` is the function body's trailing
	// expression (block-as-expression value rule), not a statement.
	if len(fn.Body.Stmts) != 0 {
		t.Errorf("body stmts = %d; want 0 (call is the trailing expr)", len(fn.Body.Stmts))
	}
	if fn.Body.Trailing == nil {
		t.Errorf("body trailing = nil; want the println call")
	}
}

func TestFizzBuzzShape(t *testing.T) {
	src := `import fmt

func main() {
  for i in 1..=100 {
    if i % 15 == 0 {
      fmt.println("FizzBuzz")
    } else if i % 3 == 0 {
      fmt.println("Fizz")
    } else if i % 5 == 0 {
      fmt.println("Buzz")
    } else {
      fmt.println(i)
    }
  }
}
`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("main body should have one for-stmt; got %d", len(fn.Body.Stmts))
	}
	fs, ok := fn.Body.Stmts[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("top-level stmt is not ForStmt: %T", fn.Body.Stmts[0])
	}
	r, ok := fs.Iterable.(*ast.RangeExpr)
	if !ok {
		t.Fatalf("iterable is not RangeExpr: %T", fs.Iterable)
	}
	if !r.Inclusive {
		t.Errorf("range should be inclusive (..=); got exclusive")
	}
}

func TestParseFile_FilePrefix(t *testing.T) {
	toks, _ := lexer.LexFile("##", "foo.aril") // lex error first
	if toks == nil {
		// expected: lex returns tokens-so-far + error; here it
		// errors before producing any token, but tokens may be
		// the empty slice with an EOF. The point of this test is
		// just to confirm the parser is reachable; if the lexer
		// failed, that's fine.
		return
	}
	_, perr := ParseFile(toks, "foo.aril")
	if perr == nil {
		return
	}
	if !strings.HasPrefix(perr.Error(), "foo.aril:") {
		t.Errorf("parser diag missing file prefix: %s", perr.Error())
	}
}

func TestComparisonNonAssociative(t *testing.T) {
	// grammar.ebnf separates EqExpr (== !=) and CmpExpr (< <= > >=)
	// as non-associative single-operator productions. Chained
	// applications at the same level must be rejected.
	cases := []string{
		`func main() { if a == b == c { } }`,
		`func main() { if a != b != c { } }`,
		`func main() { if a < b < c { } }`,
		`func main() { if a <= b > c { } }`,
	}
	for _, src := range cases {
		toks, _ := lexer.Lex(src)
		_, err := Parse(toks)
		if err == nil {
			t.Errorf("expected E0112 on %q; got no error", src)
			continue
		}
		if err.Code != "E0112" {
			t.Errorf("for %q want E0112; got %s", src, err.Code)
		}
	}
}

func TestEqAndCmpMixedNests(t *testing.T) {
	// `a == b && c < d` is fine — different precedence levels.
	src := `func main() { if a == b && c < d { } }`
	toks, _ := lexer.Lex(src)
	if _, err := Parse(toks); err != nil {
		t.Errorf("unexpected error on %q: %v", src, err)
	}
}

func TestFuncWithParamsAndReturn(t *testing.T) {
	src := `func add(a: int, b: int): int {
  return a + b
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	if fn.Name != "add" {
		t.Errorf("func name = %q; want add", fn.Name)
	}
	if len(fn.Params) != 2 {
		t.Fatalf("params count = %d; want 2", len(fn.Params))
	}
	if fn.Params[0].Name != "a" || fn.Params[1].Name != "b" {
		t.Errorf("param names = [%q, %q]; want [a, b]", fn.Params[0].Name, fn.Params[1].Name)
	}
	if fn.ReturnType == nil {
		t.Fatal("expected non-nil return type")
	}
	pt, ok := fn.ReturnType.(*ast.PrimitiveType)
	if !ok || pt.Name != "int" {
		t.Errorf("return type = %T %v; want PrimitiveType(int)", fn.ReturnType, fn.ReturnType)
	}
	// Body has one ExprStmt wrapping a ReturnExpr.
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("body stmts = %d; want 1", len(fn.Body.Stmts))
	}
	es, ok := fn.Body.Stmts[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("first stmt is not ExprStmt: %T", fn.Body.Stmts[0])
	}
	if _, ok := es.Expr.(*ast.ReturnExpr); !ok {
		t.Errorf("first stmt's expr is not ReturnExpr: %T", es.Expr)
	}
}

func TestLetVarAssign(t *testing.T) {
	src := `func main() {
  let x = 42
  var y: int = 7
  y = y + x
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	stmts := fn.Body.Stmts
	if len(stmts) != 3 {
		t.Fatalf("expected 3 stmts; got %d", len(stmts))
	}
	if let, ok := stmts[0].(*ast.LetStmt); !ok {
		t.Errorf("stmt[0] = %T %v; want LetStmt", stmts[0], stmts[0])
	} else if id, ok := let.Pattern.(*ast.IdentPat); !ok || id.Name != "x" {
		t.Errorf("LetStmt pattern = %T %v; want IdentPat x", let.Pattern, let.Pattern)
	}
	if v, ok := stmts[1].(*ast.VarStmt); !ok || v.Name != "y" || v.DeclType == nil {
		t.Errorf("stmt[1] = %T %v; want VarStmt y with type", stmts[1], stmts[1])
	}
	if _, ok := stmts[2].(*ast.AssignStmt); !ok {
		t.Errorf("stmt[2] = %T; want AssignStmt", stmts[2])
	}
}

func TestBareReturn(t *testing.T) {
	src := `func foo() {
  return
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt; got %d", len(fn.Body.Stmts))
	}
	es, ok := fn.Body.Stmts[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("not ExprStmt: %T", fn.Body.Stmts[0])
	}
	ret, ok := es.Expr.(*ast.ReturnExpr)
	if !ok {
		t.Fatalf("not ReturnExpr: %T", es.Expr)
	}
	if ret.Value != nil {
		t.Errorf("bare return has non-nil value: %v", ret.Value)
	}
}

func TestGenericTypeArgs(t *testing.T) {
	// Type-arg parsing exists even though no PR-F1 corpus uses it;
	// `Map<string, int>` should round-trip through the parser.
	src := `func lookup(m: Map<string, int>, k: string): int {
  return 0
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	mapTy, ok := fn.Params[0].DeclType.(*ast.NamedType)
	if !ok {
		t.Fatalf("first param type is not NamedType: %T", fn.Params[0].DeclType)
	}
	if mapTy.QName[0] != "Map" || len(mapTy.Args) != 2 {
		t.Errorf("Map<string, int> mis-parsed: qname=%v args=%d", mapTy.QName, len(mapTy.Args))
	}
}

func TestSumTypeNullary(t *testing.T) {
	src := `type Color = | Red | Green | Blue`
	f := parseString(t, src)
	td, ok := f.Decls[0].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("decl[0] = %T; want TypeDecl", f.Decls[0])
	}
	if td.Name != "Color" {
		t.Errorf("type name = %q; want Color", td.Name)
	}
	sb, ok := td.Body.(*ast.SumTypeBody)
	if !ok {
		t.Fatalf("body = %T; want SumTypeBody", td.Body)
	}
	if len(sb.Variants) != 3 {
		t.Errorf("variants = %d; want 3", len(sb.Variants))
	}
	for i, want := range []string{"Red", "Green", "Blue"} {
		if sb.Variants[i].Name != want {
			t.Errorf("variant[%d] = %q; want %q", i, sb.Variants[i].Name, want)
		}
		if len(sb.Variants[i].Fields) != 0 {
			t.Errorf("nullary variant has fields: %v", sb.Variants[i].Fields)
		}
	}
}

func TestMatchExpression(t *testing.T) {
	src := `func main() {
  match x {
    Red => 1,
    _ => 0,
  }
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	// The `match` is the function body's trailing (value) expression.
	m, ok := fn.Body.Trailing.(*ast.MatchExpr)
	if !ok {
		t.Fatalf("body trailing = %T; want MatchExpr", fn.Body.Trailing)
	}
	if len(m.Arms) != 2 {
		t.Fatalf("arms = %d; want 2", len(m.Arms))
	}
	if _, ok := m.Arms[0].Pattern.(*ast.VariantPat); !ok {
		t.Errorf("arm[0] pattern = %T; want VariantPat", m.Arms[0].Pattern)
	}
	if _, ok := m.Arms[1].Pattern.(*ast.WildcardPat); !ok {
		t.Errorf("arm[1] pattern = %T; want WildcardPat", m.Arms[1].Pattern)
	}
}

func TestVariantPatWithPayload(t *testing.T) {
	src := `func f() {
  match v {
    Some(x) => x,
    None => 0,
  }
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	m := fn.Body.Trailing.(*ast.MatchExpr)
	vp, ok := m.Arms[0].Pattern.(*ast.VariantPat)
	if !ok {
		t.Fatalf("arm[0] not VariantPat: %T", m.Arms[0].Pattern)
	}
	if len(vp.QName) != 1 || vp.QName[0] != "Some" || len(vp.Sub) != 1 {
		t.Errorf("Some(x) parsed as %v with %d sub-patterns", vp.QName, len(vp.Sub))
	}
	if _, ok := vp.Sub[0].(*ast.IdentPat); !ok {
		t.Errorf("Some sub-pattern = %T; want IdentPat", vp.Sub[0])
	}
}

func TestAliasTypeDecl(t *testing.T) {
	src := `type Age = int`
	f := parseString(t, src)
	td := f.Decls[0].(*ast.TypeDecl)
	ab, ok := td.Body.(*ast.AliasBody)
	if !ok {
		t.Fatalf("body = %T; want AliasBody", td.Body)
	}
	pt, ok := ab.Aliased.(*ast.PrimitiveType)
	if !ok || pt.Name != "int" {
		t.Errorf("aliased = %T %v; want PrimitiveType(int)", ab.Aliased, ab.Aliased)
	}
}

func TestSliceTypeAndLit(t *testing.T) {
	src := `func main() {
  var xs: []int = []int{1, 2, 3}
}`
	f := parseString(t, src)
	fn := f.Decls[0].(*ast.FuncDecl)
	vs, ok := fn.Body.Stmts[0].(*ast.VarStmt)
	if !ok {
		t.Fatalf("stmt[0] = %T; want VarStmt", fn.Body.Stmts[0])
	}
	st, ok := vs.DeclType.(*ast.SliceType)
	if !ok {
		t.Fatalf("decl type = %T; want SliceType", vs.DeclType)
	}
	pt, ok := st.Elem.(*ast.PrimitiveType)
	if !ok || pt.Name != "int" {
		t.Errorf("elem = %T %v; want PrimitiveType(int)", st.Elem, st.Elem)
	}
	sl, ok := vs.Value.(*ast.SliceLit)
	if !ok {
		t.Fatalf("value = %T; want SliceLit", vs.Value)
	}
	if sl.ElemType == nil {
		t.Errorf("annotated SliceLit lost ElemType")
	}
	if len(sl.Items) != 3 {
		t.Errorf("items = %d; want 3", len(sl.Items))
	}
}

func TestIndexAndSlice(t *testing.T) {
	src := `func main() {
  let v = xs[0]
  let mid = xs[1:3]
  let suf = xs[1:]
  let pre = xs[:3]
}`
	f := parseString(t, src)
	stmts := f.Decls[0].(*ast.FuncDecl).Body.Stmts
	// First: Index
	if _, ok := stmts[0].(*ast.LetStmt).Value.(*ast.Index); !ok {
		t.Errorf("stmt[0] value = %T; want Index", stmts[0].(*ast.LetStmt).Value)
	}
	// Slice forms
	for i := 1; i < 4; i++ {
		if _, ok := stmts[i].(*ast.LetStmt).Value.(*ast.Slice); !ok {
			t.Errorf("stmt[%d] value = %T; want Slice", i, stmts[i].(*ast.LetStmt).Value)
		}
	}
	// `xs[1:]` — High is nil
	if se := stmts[2].(*ast.LetStmt).Value.(*ast.Slice); se.High != nil {
		t.Errorf("xs[1:] has non-nil High")
	}
	// `xs[:3]` — Low is nil
	if se := stmts[3].(*ast.LetStmt).Value.(*ast.Slice); se.Low != nil {
		t.Errorf("xs[:3] has non-nil Low")
	}
}

func TestInferredSliceLit(t *testing.T) {
	src := `func main() {
  let xs = [10, 20, 30]
}`
	f := parseString(t, src)
	let := f.Decls[0].(*ast.FuncDecl).Body.Stmts[0].(*ast.LetStmt)
	sl, ok := let.Value.(*ast.SliceLit)
	if !ok {
		t.Fatalf("value = %T; want SliceLit", let.Value)
	}
	if sl.ElemType != nil {
		t.Errorf("inferred SliceLit unexpectedly has ElemType")
	}
	if len(sl.Items) != 3 {
		t.Errorf("items = %d; want 3", len(sl.Items))
	}
}

func TestCanonicalSerialisationStable(t *testing.T) {
	src := `import fmt

func main() {
  fmt.println("Aril is rising.")
}
`
	f := parseString(t, src)
	a := ast.Canonical(f)
	// Second parse → second canonical must equal first.
	g := parseString(t, src)
	b := ast.Canonical(g)
	if a != b {
		t.Errorf("canonical serialisation not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	// Sanity: form starts with the root node name.
	if !strings.HasPrefix(a, "(File") {
		t.Errorf("canonical does not start with (File: %s", a[:40])
	}
}

// splitChainedTupleIndex re-splits a `N.M` FloatLit lexeme that the
// context-free lexer produced for a chained tuple index `r.1.0`. Only the
// plain `digits "." digits` shape is a chain; every other float form (and
// malformed input) must be rejected so the parser can report the normal
// "expected field name after `.`" diagnostic.
func TestSplitChainedTupleIndex(t *testing.T) {
	cases := []struct {
		lexeme   string
		lhs, rhs int
		ok       bool
	}{
		{"1.0", 1, 0, true},
		{"0.0", 0, 0, true},
		{"10.2", 10, 2, true},
		{"1e3", 0, 0, false},   // exponent, not a chain
		{"1.5e3", 0, 0, false}, // fractional exponent
		{"1.", 0, 0, false},    // missing rhs
		{".5", 0, 0, false},    // missing lhs
		{"1.2.3", 0, 0, false}, // rhs "2.3" is not an integer
		{"12", 0, 0, false},    // no dot
	}
	for _, c := range cases {
		lhs, rhs, ok := splitChainedTupleIndex(c.lexeme)
		if ok != c.ok || (ok && (lhs != c.lhs || rhs != c.rhs)) {
			t.Errorf("splitChainedTupleIndex(%q) = (%d, %d, %v), want (%d, %d, %v)",
				c.lexeme, lhs, rhs, ok, c.lhs, c.rhs, c.ok)
		}
	}
}

// The arrow func-type `(A) => R` shares the `(` prefix with a TupleType; the
// trailing `=>` is what disambiguates. A one-element paren-list is a
// func-type with `=>` but a rejected 1-tuple without it.
func TestArrowFuncType(t *testing.T) {
	f := parseString(t, "func apply(g: (int) => bool): int { return 0 }\n")
	fn := f.Decls[0].(*ast.FuncDecl)
	ft, ok := fn.Params[0].DeclType.(*ast.FuncType)
	if !ok {
		t.Fatalf("param type not FuncType: %T", fn.Params[0].DeclType)
	}
	if len(ft.Params) != 1 {
		t.Fatalf("arrow func-type param count = %d, want 1", len(ft.Params))
	}
	if ft.ReturnType == nil {
		t.Fatalf("arrow func-type missing return type")
	}

	// Without `=>`, a one-element paren-list stays a rejected 1-tuple.
	toks, _ := lexer.Lex("func f(x: (int)): int { return 0 }\n")
	_, perr := Parse(toks)
	if perr == nil {
		t.Fatalf("expected 1-tuple type error, got nil")
	}
	if perr.Code != "E0112" {
		t.Fatalf("expected E0112 1-tuple error, got %s", perr.Code)
	}
}

// TestSelectBracelessBody — a select case admits a braceless single-stmt
// body (`=> return …`, `=> x = y`) as well as a brace block, parsing it
// into the same `*ast.Block` shape (grammar.ebnf §SelectCaseBody).
func TestSelectBracelessBody(t *testing.T) {
	cases := []string{
		`func f(ch: Channel<int>): int { select { case v = <-ch => return v, default => return 0, } }`,
		`func f(ch: Channel<int>): unit { select { case <-ch => sum = sum + 1, default => { }, } }`,
		`func f(ch: Channel<int>): unit { select { case ch.send(1) => done = true, default => { }, } }`,
	}
	for _, src := range cases {
		toks, lerr := lexer.Lex(src)
		if lerr != nil {
			t.Fatalf("lex %q: %v", src, lerr)
		}
		f, err := Parse(toks)
		if err != nil {
			t.Errorf("unexpected error on %q: %v", src, err)
			continue
		}
		sel := f.Decls[0].(*ast.FuncDecl).Body.Stmts[0].(*ast.SelectStmt)
		for _, c := range sel.Cases {
			var body *ast.Block
			switch sc := c.(type) {
			case *ast.SelectRecv:
				body = sc.Body
			case *ast.SelectSend:
				body = sc.Body
			case *ast.SelectDefault:
				body = sc.Body
			}
			if body == nil {
				t.Errorf("%q: select case has nil body", src)
			}
		}
	}
}

// TestSeparableBlocksOutOfDecls checks that both separable contract surfaces
// — `contract` (RFC-0006) and `channel` (RFC-0007) — parse into their side
// tables (File.Contracts / File.Channels) and stay *out* of File.Decls, so
// codegen/sema (which iterate Decls) lower byte-identically until the contract
// passes consume them.
func TestSeparableBlocksOutOfDecls(t *testing.T) {
	src := `func abs(x: int): int {
  return x
}

contract abs {
  requires x >= 0
  // a match predicate nests braces — the clause loop balances them
  ensures match result { Ok(q) => q >= 0, Err(_) => true }
}

channel results {
  closed-by pool
  forbid send after close
}

func id(y: int): int { return y }
`
	f := parseString(t, src)
	if len(f.Decls) != 2 {
		t.Fatalf("expected 2 decls (contract/channel blocks live in side tables); got %d", len(f.Decls))
	}
	for i, want := range []string{"abs", "id"} {
		fn, ok := f.Decls[i].(*ast.FuncDecl)
		if !ok {
			t.Fatalf("decl[%d] not FuncDecl: %T", i, f.Decls[i])
		}
		if fn.Name != want {
			t.Errorf("decl[%d] name = %q, want %q", i, fn.Name, want)
		}
	}
	if len(f.Contracts) != 1 {
		t.Errorf("expected 1 contract in File.Contracts; got %d", len(f.Contracts))
	}
	if len(f.Channels) != 1 {
		t.Errorf("expected 1 channel in File.Channels; got %d", len(f.Channels))
	}
}

// TestChannelBlockUnterminated reports a clean diagnostic (not a panic or
// silent EOF) when a `channel` block's brace is never closed — the clause
// loop reaches EOF and `expect("}")` surfaces a closing-brace diagnostic.
func TestChannelBlockUnterminated(t *testing.T) {
	src := `func main() {}

channel main {
  closed-by pool
`
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		t.Fatalf("lex error: %v", lerr)
	}
	_, perr := Parse(toks)
	if perr == nil {
		t.Fatal("expected a diagnostic for the unterminated channel block")
	}
	if perr.Code != "E0112" || !strings.Contains(perr.Message, "}") {
		t.Errorf("diagnostic = %q (%s), want an E0112 mentioning the missing `}`", perr.Message, perr.Code)
	}
}

// TestContractIdentifierNotClaimed guards the positional claim: outside
// the top-level `contract <name> {` shape, `contract`/`channel` stay
// ordinary identifiers (here a local binding name).
func TestContractIdentifierNotClaimed(t *testing.T) {
	src := `func main() {
  let contract = 1
  let channel = 2
}
`
	f := parseString(t, src)
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 decl; got %d", len(f.Decls))
	}
}

// TestLoopLabel covers the optional `loop <label>` on for/while headers
// (RFC-0006 loop invariants) and the contextual guard: `loop` stays an
// ordinary identifier when it is the iterable/condition itself, not a label.
func TestLoopLabel(t *testing.T) {
	f := parseString(t, `func main() {
  for i in xs loop scan { print(i) }
  while go loop spin { stop() }
}
`)
	body := f.Decls[0].(*ast.FuncDecl).Body.Stmts
	fs, ok := body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("stmt[0] not ForStmt: %T", body[0])
	}
	if fs.Label != "scan" {
		t.Errorf("for label = %q, want %q", fs.Label, "scan")
	}
	ws, ok := body[1].(*ast.WhileStmt)
	if !ok {
		t.Fatalf("stmt[1] not WhileStmt: %T", body[1])
	}
	if ws.Label != "spin" {
		t.Errorf("while label = %q, want %q", ws.Label, "spin")
	}

	// `loop` as the iterable itself (a bare name ending the header) must NOT
	// be eaten as a label — there is no identifier after it before the block.
	g := parseString(t, `func main() {
  for x in loop { use(x) }
}
`)
	fs2 := g.Decls[0].(*ast.FuncDecl).Body.Stmts[0].(*ast.ForStmt)
	if fs2.Label != "" {
		t.Errorf("for-in-loop: label = %q, want empty (loop is the iterable)", fs2.Label)
	}
}

// TestContractBlockParsed covers C3b: a `contract` block parses into
// File.Contracts (RFC-0006 value/state clauses), alongside a `channel` block
// in File.Channels (RFC-0007).
func TestContractBlockParsed(t *testing.T) {
	f := parseString(t, `func abs(x: int): int { return x }

contract abs {
  requires x >= 0
  ensures result >= 0
  loop scan {
    invariant x >= 0
  }
}

channel results { closed-by pool }
`)
	if len(f.Decls) != 1 {
		t.Fatalf("expected 1 decl (the func); got %d", len(f.Decls))
	}
	if len(f.Contracts) != 1 {
		t.Fatalf("expected 1 contract; got %d", len(f.Contracts))
	}
	c := f.Contracts[0]
	if c.Target != "abs" {
		t.Errorf("contract target = %q, want %q", c.Target, "abs")
	}
	if len(c.Clauses) != 3 {
		t.Fatalf("expected 3 clauses (requires/ensures/loop); got %d", len(c.Clauses))
	}
	kinds := []string{c.Clauses[0].Kind, c.Clauses[1].Kind, c.Clauses[2].Kind}
	want := []string{"requires", "ensures", "loop"}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("clause[%d] kind = %q, want %q", i, kinds[i], want[i])
		}
	}
	loop := c.Clauses[2]
	if loop.Label != "scan" {
		t.Errorf("loop label = %q, want %q", loop.Label, "scan")
	}
	if len(loop.Loop) != 1 || loop.Loop[0].Kind != "invariant" {
		t.Errorf("loop section = %+v, want one invariant clause", loop.Loop)
	}
}

// TestChannelBlockParsed covers C7a-i: a `channel` block parses into
// File.Channels with the six local channel clauses (RFC-0007 §Design). The
// hyphenated phrase-keywords (`closed-by`, `drains-before-scope-exit`) lex as
// `ident - ident` runs and are matched as lexeme sequences.
func TestChannelBlockParsed(t *testing.T) {
	f := parseString(t, `func run(jobs: []int) { return }

channel results {
  closed-by pool
  forbid send after close
  forbid recv after close
  never more than jobs.len() in flight
  drains-before-scope-exit
  drains-before-return
}
`)
	if len(f.Channels) != 1 {
		t.Fatalf("expected 1 channel in File.Channels; got %d", len(f.Channels))
	}
	ch := f.Channels[0]
	if ch.Subject != "results" {
		t.Errorf("channel subject = %q, want %q", ch.Subject, "results")
	}
	wantKinds := []string{
		"closed-by", "forbid-send-after-close", "forbid-recv-after-close",
		"capacity", "drains-before-scope-exit", "drains-before-return",
	}
	if len(ch.Clauses) != len(wantKinds) {
		t.Fatalf("expected %d channel clauses; got %d", len(wantKinds), len(ch.Clauses))
	}
	for i, want := range wantKinds {
		if ch.Clauses[i].Kind != want {
			t.Errorf("clause[%d] kind = %q, want %q", i, ch.Clauses[i].Kind, want)
		}
	}
	if ch.Clauses[0].Owner != "pool" {
		t.Errorf("closed-by owner = %q, want %q", ch.Clauses[0].Owner, "pool")
	}
	if ch.Clauses[3].Bound == nil {
		t.Error("capacity clause has no bound expression")
	}
}

// TestContractProtocolClauses covers C7a-ii: a `contract` body hosts the
// RFC-0007 cross-channel protocol clauses (subject decls, two-event
// ordering/liveness, fan-out, fairness) alongside value/state clauses.
func TestContractProtocolClauses(t *testing.T) {
	f := parseString(t, `func run() { return }

contract WorkerPool {
  channel work
  channel done role cancel
  participant subscribers: Set<Subscriber>
  forbid results.send(Result{id}) before work.recv(Job{id})
  eventually results.close after work.close
  every work.recv(Job{id}) eventually results.send(Result{id})
  deadline delivered-to-all { producer, consumer }
  messages delivered-to-all subscribers
  fairness { no-starvation inputA }
}
`)
	if len(f.Contracts) != 1 {
		t.Fatalf("expected 1 contract; got %d", len(f.Contracts))
	}
	cls := f.Contracts[0].Clauses
	wantKinds := []string{
		"channel-subject", "channel-subject", "participant",
		"forbid-before", "eventually-after", "every-eventually",
		"delivered-to-all", "delivered-to-all", "fairness",
	}
	if len(cls) != len(wantKinds) {
		t.Fatalf("expected %d clauses; got %d", len(wantKinds), len(cls))
	}
	for i, want := range wantKinds {
		if cls[i].Kind != want {
			t.Errorf("clause[%d] kind = %q, want %q", i, cls[i].Kind, want)
		}
	}
	if cls[1].Role != "cancel" {
		t.Errorf("subject role = %q, want %q", cls[1].Role, "cancel")
	}
	if cls[2].PartType == nil {
		t.Error("typed participant has nil PartType")
	}
	if cls[3].EventA == nil || cls[3].EventB == nil {
		t.Error("forbid-before clause is missing an event operand")
	}
	if cls[6].Kind == "delivered-to-all" && len(cls[6].Names) != 2 {
		t.Errorf("explicit fan-out set = %v, want 2 members", cls[6].Names)
	}
	if cls[7].RecvSet != "subscribers" {
		t.Errorf("named fan-out set = %q, want %q", cls[7].RecvSet, "subscribers")
	}
	if len(cls[8].Names) != 1 || cls[8].Names[0] != "inputA" {
		t.Errorf("fairness no-starvation subjects = %v, want [inputA]", cls[8].Names)
	}
}

// TestContractProtocolBadInfix rejects a two-event clause missing its infix.
func TestContractProtocolBadInfix(t *testing.T) {
	toks, _ := lexer.Lex(`func f() {}
contract C { every a.recv(x) results.send(y) }
`)
	_, perr := Parse(toks)
	if perr == nil || !strings.Contains(perr.Message, "expected `eventually`") {
		t.Fatalf("want a missing-infix diagnostic, got %v", perr)
	}
}

// TestChannelUnknownClause rejects a clause keyword outside the v1 channel set.
func TestChannelUnknownClause(t *testing.T) {
	toks, _ := lexer.Lex(`func f() {}
channel c { bogus x }
`)
	_, perr := Parse(toks)
	if perr == nil || !strings.Contains(perr.Message, "expected a channel clause") {
		t.Fatalf("want an unknown-channel-clause diagnostic, got %v", perr)
	}
}

// TestContractUnknownClause rejects a clause keyword outside the v1 set.
func TestContractUnknownClause(t *testing.T) {
	toks, _ := lexer.Lex(`func f() {}
contract f { bogus x }
`)
	_, perr := Parse(toks)
	if perr == nil || !strings.Contains(perr.Message, "expected a contract clause") {
		t.Fatalf("want an unknown-clause diagnostic, got %v", perr)
	}
}

// TestContractLoopOnlyInvariant rejects a non-invariant clause inside a loop
// section.
func TestContractLoopOnlyInvariant(t *testing.T) {
	toks, _ := lexer.Lex(`func f() {}
contract f { loop scan { ensures result > 0 } }
`)
	_, perr := Parse(toks)
	if perr == nil || !strings.Contains(perr.Message, "only `invariant`") {
		t.Fatalf("want a loop-only-invariant diagnostic, got %v", perr)
	}
}

// TestContractEntrySection covers the `entry { let … }` section (RFC-0006
// entry snapshots) and the v1 let-only restriction (a `var` is rejected).
func TestContractEntrySection(t *testing.T) {
	f := parseString(t, `func g(x: int): int { return x }

contract g {
  entry {
    let x0 = x
  }
  ensures result == x0
}
`)
	if len(f.Contracts) != 1 {
		t.Fatalf("expected 1 contract; got %d", len(f.Contracts))
	}
	cls := f.Contracts[0].Clauses
	if len(cls) != 2 || cls[0].Kind != "entry" || cls[1].Kind != "ensures" {
		t.Fatalf("clauses = %v, want [entry ensures]", []string{cls[0].Kind, cls[1].Kind})
	}
	if len(cls[0].Bindings) != 1 || cls[0].Bindings[0].Name != "x0" {
		t.Errorf("entry binding = %+v, want one named x0", cls[0].Bindings)
	}

	// `var` in an entry section is rejected (v1 let-only).
	toks, _ := lexer.Lex(`func g(x: int): int { return x }
contract g { entry { var x0 = x } ensures result == x0 }
`)
	_, perr := Parse(toks)
	if perr == nil || !strings.Contains(perr.Message, "must be `let`") {
		t.Errorf("want a let-only diagnostic for `var` in entry, got %v", perr)
	}
}
