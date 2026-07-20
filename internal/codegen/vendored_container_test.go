package codegen

import (
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
	"github.com/aril-lang/aril/internal/sema"
)

// emitVendored lowers src in vendored-runtime mode, where container
// references carry the `arilrt.` selector — the mode the corpus and real
// programs build under, and the only one that exercises the runtime
// prefix (inline mode's prefix is empty, so the golden fixtures cannot
// catch a missing-prefix leak).
func emitVendored(t *testing.T, src string) string {
	t.Helper()
	toks, lerr := lexer.LexFile(src, "v.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "v.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	files := []*ast.File{f}
	paths := []string{"v.aril"}
	info, diags := sema.CheckFiles(files, paths)
	if len(diags) > 0 {
		t.Fatalf("sema: %v", diags)
	}
	out, err := EmitFilesWithOptions(files, paths, info, Options{
		Vendored:          true,
		RuntimeImportPath: "aril-output/arilrt",
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

// A non-empty `Map<K,V>{ k: v }` literal lowers to an insertion IIFE whose
// type and constructor must carry the runtime prefix in vendored mode; a
// bare `Map`/`NewMap` leaked a raw Go `undefined: Map` across the package
// boundary (the empty-literal path was already prefixed).
func TestVendoredMapLiteralPrefix(t *testing.T) {
	got := emitVendored(t, `import fmt
func main() {
  let m = Map<string, int>{ "a": 1, "b": 2 }
  fmt.println(m.len())
}
`)
	if strings.Contains(got, "*Map[") || strings.Contains(got, ":= NewMap[") {
		t.Errorf("non-empty Map literal leaked an unprefixed Map/NewMap:\n%s", got)
	}
	if !strings.Contains(got, "arilrt.NewMap[string, int]()") {
		t.Errorf("expected the vendored arilrt.NewMap IIFE, got:\n%s", got)
	}
}

// A `for`/index/method over a container-typed *field* (a member access,
// not a bare Ident) must route through the container intercept — the
// wrapper's `.ToSlice()`/`.Keys()`/`.At()`/`.Len()` — never a raw Go
// `range`/index/`len` over `*arilrt.List`, which leaks a go/types error
// (bug#4). Checked in vendored mode so the arilrt selector is present.
func TestVendoredContainerFieldReceiver(t *testing.T) {
	got := emitVendored(t, `import fmt
type H = { xs: List<int>, tags: Map<string, int> }
func main() {
  let h = H{ xs: List<int>{1, 2}, tags: Map<string, int>{} }
  for x in h.xs { fmt.println(x) }
  for k in h.tags { fmt.println(k) }
  let n = h.xs.len()
  let v = h.tags["a"]
  fmt.println(n)
  fmt.println(v)
}
`)
	// Field-receiver for/index/method dispatch through the wrapper API…
	for _, want := range []string{"h.Xs.ToSlice()", "h.Tags.Keys()", "h.Xs.Len()", "h.Tags.At("} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q — field receiver did not route through the container intercept:\n%s", want, got)
		}
	}
	// …and never leak the raw Go builtins over the wrapper pointer.
	for _, bad := range []string{"range h.Xs {", "len(h.Xs)", "h.Tags[\"a\"]"} {
		if strings.Contains(got, bad) {
			t.Errorf("leaked raw Go %q over a container field:\n%s", bad, got)
		}
	}
}
