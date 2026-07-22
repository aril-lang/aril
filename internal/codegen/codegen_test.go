package codegen

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

func emitString(t *testing.T, src string) string {
	t.Helper()
	return emitWithFile(t, src, "")
}

func emitWithFile(t *testing.T, src, file string) string {
	t.Helper()
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		t.Fatalf("lex error: %v", lerr)
	}
	f, perr := parser.Parse(toks)
	if perr != nil {
		t.Fatalf("parse error: %v", perr)
	}
	out, err := Emit(f, file)
	if err != nil {
		t.Fatalf("emit error: %v", err)
	}
	return out
}

// TestEmitTypeParamConstraint: an `Ordered` bound lowers to `[T cmp.Ordered]`
// and pulls in `import "cmp"`; a `Comparable` bound lowers to the Go built-in
// `[T comparable]` (no import); an unconstrained parameter stays `[T any]`.
func TestEmitTypeParamConstraint(t *testing.T) {
	ordered := emitString(t, `func isSorted<T: Ordered>(xs: []T): bool {
  for i in 1..xs.len() {
    if xs[i] < xs[i - 1] { return false }
  }
  return true
}
func main() {}
`)
	if !strings.Contains(ordered, "func isSorted[T cmp.Ordered]") {
		t.Errorf("Ordered bound did not lower to cmp.Ordered:\n%s", ordered)
	}
	if !strings.Contains(ordered, `"cmp"`) {
		t.Errorf("Ordered bound did not pull in the cmp import:\n%s", ordered)
	}

	comparable := emitString(t, `func allEq<T: Comparable>(xs: []T, v: T): bool {
  for i in 0..xs.len() { if xs[i] != v { return false } }
  return true
}
func main() {}
`)
	if !strings.Contains(comparable, "func allEq[T comparable]") {
		t.Errorf("Comparable bound did not lower to the Go comparable constraint:\n%s", comparable)
	}
	if strings.Contains(comparable, `"cmp"`) {
		t.Errorf("Comparable bound must not pull in the cmp import:\n%s", comparable)
	}

	unconstrained := emitString(t, `func id<T>(x: T): T { return x }
func main() {}
`)
	if !strings.Contains(unconstrained, "func id[T any]") {
		t.Errorf("unconstrained type param must stay [T any]:\n%s", unconstrained)
	}
}

func TestEmitHello(t *testing.T) {
	src := `import fmt

func main() {
  fmt.println("Aril is rising.")
}
`
	got := emitString(t, src)
	want := `package main

import "fmt"

func main() {
	fmt.Println("Aril is rising.")
}
`
	if got != want {
		t.Errorf("emit mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestEmitFizzBuzz(t *testing.T) {
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
	got := emitString(t, src)
	// Smoke check: contains the inclusive-range termination,
	// the chained else-if, and the Go-side fmt.Println calls.
	for _, fragment := range []string{
		"package main",
		"import \"fmt\"",
		"for i := 1; i <= 100; i++ {",
		"if i%15 == 0 {",
		"} else if i%3 == 0 {",
		"} else if i%5 == 0 {",
		"} else {",
		"fmt.Println(\"FizzBuzz\")",
		"fmt.Println(\"Fizz\")",
		"fmt.Println(\"Buzz\")",
		"fmt.Println(i)",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("emit missing fragment %q. Full:\n%s", fragment, got)
		}
	}
}

// TestEmitGofmtStable verifies the gofmt -s round-trip property
// stated in lang-spec/test-contract.md §GO and
// lang-spec/lowering-go.md §Output formatting.
func TestEmitGofmtStable(t *testing.T) {
	cases := []string{
		`func main() {
  fmt.println("hi")
}`,
		`func main() {
  for i in 0..10 {
    if i % 2 == 0 {
      fmt.println(i)
    }
  }
}`,
	}
	for _, src := range cases {
		out := emitString(t, "import fmt\n\n"+src+"\n")
		formatted, err := format.Source([]byte(out))
		if err != nil {
			t.Errorf("emitted code does not parse:\n%s\nerror: %v", out, err)
			continue
		}
		if string(formatted) != out {
			t.Errorf("emit is not gofmt-stable.\n--- emit ---\n%s\n--- gofmt ---\n%s",
				out, formatted)
		}
	}
}

// TestEmitCompiles writes the emitted Go to a temp file and runs
// `go build` on it; failure points at a real codegen bug.
func TestEmitCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available; skip compile check")
	}
	src := `import fmt

func main() {
  fmt.println("hi")
}
`
	out := emitString(t, src)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Need a go.mod so `go build` works on the temp dir.
	mod := "module aril-codegen-test\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", "/dev/null", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go build failed: %v\n%s", err, out)
	}
}

// TestEmitWithFilePath exercises the load-bearing //line path
// (D8 / lowering-go.md §Source maps). Verifies (a) the directives
// appear, (b) the output remains gofmt-stable, (c) Go still
// compiles it.
func TestEmitWithFilePath(t *testing.T) {
	src := `import fmt

func main() {
  for i in 1..=3 {
    if i == 1 {
      fmt.println("a")
    } else if i == 2 {
      fmt.println("b")
    } else {
      fmt.println("c")
    }
  }
}
`
	out := emitWithFile(t, src, "src.aril")
	// Must contain //line directives — at least one per
	// top-level statement, and one for each else-if's nested
	// condition.
	if !strings.Contains(out, "//line src.aril:3:1") {
		t.Errorf("missing //line for func main: %s", out)
	}
	if !strings.Contains(out, "//line src.aril:4:1") {
		t.Errorf("missing //line for the for-loop: %s", out)
	}
	if !strings.Contains(out, "//line src.aril:5:1") {
		t.Errorf("missing //line for the if condition: %s", out)
	}
	if !strings.Contains(out, "//line src.aril:7:1") {
		t.Errorf("missing //line for else-if at line 7: %s", out)
	}
	// gofmt-stability — Emit already pipes through go/format, so
	// it should be byte-stable. Confirm.
	formatted, err := format.Source([]byte(out))
	if err != nil {
		t.Fatalf("emit-with-//line failed to parse: %v", err)
	}
	if string(formatted) != out {
		t.Errorf("emit-with-//line is not gofmt-stable:\n--- emit ---\n%s\n--- gofmt ---\n%s", out, formatted)
	}
	// Compile sanity.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available; skip compile check")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module aril-codegen-test\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", "/dev/null", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("emit-with-//line failed to compile: %v\n%s", err, out)
	}
}

// TestEmitRunHello does a full compile-and-run, checking STDOUT
// matches the expected line. PR-D will wire this into the
// fixture runner.
func TestEmitRunHello(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	src := `import fmt

func main() {
  fmt.println("Aril is rising.")
}
`
	out := emitString(t, src)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := "module aril-codegen-test\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmd := exec.Command("go", "run", "./...")
	cmd.Dir = dir
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run failed: %v", err)
	}
	if got := string(stdout); got != "Aril is rising.\n" {
		t.Errorf("hello stdout = %q; want %q", got, "Aril is rising.\n")
	}
}

// TestEmitRunCompositeString is the D56 end-to-end guard: a program that
// prints composites must render the Aril value (via the generated
// fmt.Stringer String()), not Go's raw `%v` lowering (`&{[1 2 3]}` etc.).
// Compile-and-run in inline mode so the inline-prelude String() methods are
// exercised (vendored mode is covered by arilrt's unit tests + the corpus).
func TestEmitRunCompositeString(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	src := `import fmt

func main() {
  fmt.println(List<int>{1, 2, 3})
  let m = Map<string, int>.new()
  m.set("a", 1)
  fmt.println(m)
  let s = Set<int>.new()
  s.add(7)
  fmt.println(s)
  fmt.println(Some(5))
  let r: Result<int, string> = Ok(9)
  fmt.println(r)
  fmt.println(List<Option<int>>{Some(1), Some(2)})
}
`
	out := emitString(t, src)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := "module aril-codegen-test\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmd := exec.Command("go", "run", "./...")
	cmd.Dir = dir
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run failed: %v", err)
	}
	want := "[1, 2, 3]\n{a: 1}\n{7}\nSome(5)\nOk(9)\n[Some(1), Some(2)]\n"
	if got := string(stdout); got != want {
		t.Errorf("composite stdout =\n%q\nwant\n%q", got, want)
	}
}

// TestEmitRunUserTypeString is the D56 guard for the codegen-generated
// String() on user records + sum types: records render by field name, sum
// variants by name (payload positional, nullary bare), recursive sums recurse
// through the *T self-ref field. Compile-and-run so the emitted method and its
// fmt import are actually exercised (not just string-compared).
func TestEmitRunUserTypeString(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	src := `import fmt

type Point = { x: int, y: int }

type Tree =
  | Leaf
  | Node(value: int, left: Tree, right: Tree)

func main() {
  fmt.println(Point{x: 1, y: 2})
  fmt.println(Node(5, Node(3, Leaf, Leaf), Leaf))
  fmt.println(Leaf)
}
`
	out := emitString(t, src)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := "module aril-codegen-test\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmd := exec.Command("go", "run", "./...")
	cmd.Dir = dir
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run failed: %v", err)
	}
	want := "{x: 1, y: 2}\nNode(5, Node(3, Leaf, Leaf), Leaf)\nLeaf\n"
	if got := string(stdout); got != want {
		t.Errorf("user-type stdout =\n%q\nwant\n%q", got, want)
	}
}

// TestRecordStringFieldClashSkipsStringer guards the D56 collision edge: a
// record field whose exported Go name is `String` would clash with the
// generated method, so no String() is emitted (Go's default rendering stays)
// — the emitted Go must still compile.
func TestRecordStringFieldClashSkipsStringer(t *testing.T) {
	src := `type Weird = { string: int, y: int }
`
	out := emitString(t, src)
	if strings.Contains(out, "func (_arilSelf Weird) String()") {
		t.Errorf("String() must be skipped for a record with a `String`-named field, got:\n%s", out)
	}
}

// TestErrorCtorLowersToErrorsNew locks the `error(msg)` free-constructor
// lowering and its shadow-safety: the genuine builtin lowers to
// errors.New, but a user decl that shadows `error` is called normally
// (gated on the sema SymBuiltinType symbol, mirroring the refEq intercept).
func TestErrorCtorLowersToErrorsNew(t *testing.T) {
	src := `func make_err(): Result<int, error> {
  return Err(error("bad"))
}
`
	out := emitString(t, src)
	if !strings.Contains(out, `errors.New("bad")`) {
		t.Errorf("expected errors.New lowering, got:\n%s", out)
	}
}

func TestUserErrorDeclNotHijacked(t *testing.T) {
	src := `func error(msg: string): int { return 42 }

func use(): int { return error("hello") }
`
	out := emitString(t, src)
	if strings.Contains(out, "errors.New") {
		t.Errorf("user-declared error must not be rewritten to errors.New, got:\n%s", out)
	}
	if !strings.Contains(out, `error("hello")`) {
		t.Errorf("expected the user error func to be called, got:\n%s", out)
	}
}
