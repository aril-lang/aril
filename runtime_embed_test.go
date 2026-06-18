package aril

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteVendoredRuntime(t *testing.T) {
	dir := t.TempDir()
	n, err := WriteVendoredRuntime(dir)
	if err != nil {
		t.Fatalf("WriteVendoredRuntime: %v", err)
	}
	if n == 0 {
		t.Fatal("no runtime files written")
	}

	pkgDir := filepath.Join(dir, "arilrt")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read vendored dir: %v", err)
	}

	fset := token.NewFileSet()
	sawRuntime := false
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			t.Errorf("test file leaked into vendored runtime: %s", name)
		}
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if name == "runtime.go" {
			sawRuntime = true
		}
		// Every vendored file must parse as Go in package arilrt — a
		// corrupt copy would surface here rather than at `go run` time.
		path := filepath.Join(pkgDir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			t.Errorf("vendored %s does not parse: %v", name, err)
			continue
		}
		if f.Name.Name != "arilrt" {
			t.Errorf("vendored %s has package %q, want arilrt", name, f.Name.Name)
		}
	}
	if !sawRuntime {
		t.Error("runtime.go missing from vendored output")
	}
}
