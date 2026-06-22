package codegen

import (
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
	"github.com/aril-lang/aril/internal/sema"
)

func emitContract(t *testing.T, src, mode string) string {
	t.Helper()
	toks, lerr := lexer.LexFile(src, "c.aril")
	if lerr != nil {
		t.Fatalf("lex: %v", lerr)
	}
	f, perr := parser.ParseFile(toks, "c.aril")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	files := []*ast.File{f}
	paths := []string{"c.aril"}
	info, diags := sema.CheckFiles(files, paths)
	if len(diags) > 0 {
		t.Fatalf("sema: %v", diags)
	}
	out, err := EmitFilesWithOptions(files, paths, info, Options{ContractMode: mode})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

// TestEmitLoopInvariant: under --contracts=panic a loop invariant lowers to a
// per-iteration `if !(pred) { panic(...) }`; under off nothing is emitted
// (byte-identical lowering).
func TestEmitLoopInvariant(t *testing.T) {
	src := `func run(xs: []int): int {
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
`
	panicMode := emitContract(t, src, "panic")
	if !strings.Contains(panicMode, "if !(acc >= 0)") {
		t.Errorf("panic-mode emit missing the invariant check:\n%s", panicMode)
	}
	if !strings.Contains(panicMode, "loop invariant violated (loop scan)") {
		t.Errorf("panic-mode emit missing the blame message:\n%s", panicMode)
	}

	off := emitContract(t, src, "off")
	if strings.Contains(off, "loop invariant violated") {
		t.Errorf("off-mode emit must not contain a contract check:\n%s", off)
	}
	// off mode is byte-identical to the same program without the contract.
	noContract := emitContract(t, `func run(xs: []int): int {
  var acc = 0
  for i in 0..xs.len() loop scan {
    acc = acc + xs[i]
  }
  return acc
}
`, "off")
	if off != noContract {
		t.Errorf("off-mode emit differs from the contract-free program:\n--- with ---\n%s\n--- without ---\n%s", off, noContract)
	}
}
