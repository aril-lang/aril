// Command aril is the compiler and toolchain for the Aril programming language.
//
// Aril is pre-alpha. Three subcommands wire the lexer / parser / codegen
// pipeline:
//
//	aril emit  <file.aril>             print the lowered Go source to stdout
//	aril build [-o out] <file.aril>    compile to a Go binary (default: ./<basename>)
//	aril run   <file.aril>             compile and execute (stdio passed through,
//	                                 exit code propagated)
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	aril "github.com/aril-lang/aril"
	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/bindgen"
	"github.com/aril-lang/aril/internal/codegen"
	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
	"github.com/aril-lang/aril/internal/sema"
)

const version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Printf("aril %s\n", version)
	case "emit":
		os.Exit(cmdEmit(os.Args[2:]))
	case "build":
		os.Exit(cmdBuild(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "repl":
		os.Exit(cmdRepl(os.Args[2:]))
	case "import":
		os.Exit(cmdImport(os.Args[2:]))
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "aril: unknown subcommand %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

// addContractsFlag registers `--contracts=<mode>` on fs and returns the
// destination. The four modes are RFC-0006's panic/warn/stats/off; only
// `off` (the parser's contract-skip "ignore level") is wired during the
// CONTRACTS-IMPL build-out — checkContractsMode rejects the others until
// the enforcement pipeline lands.
func addContractsFlag(fs *flag.FlagSet) *string {
	return fs.String("contracts", "off",
		"contract enforcement mode: off (skip, default) | panic | warn | stats")
}

// checkContractsMode validates a --contracts value. off is the only wired
// mode for now; panic/warn/stats are accepted vocabulary but not yet
// enforced; anything else is a usage error.
func checkContractsMode(mode string) error {
	switch mode {
	case "off":
		return nil
	case "panic", "warn", "stats":
		return fmt.Errorf("aril: --contracts=%s is not yet implemented "+
			"(CONTRACTS-IMPL in progress); only --contracts=off is wired", mode)
	default:
		return fmt.Errorf("aril: unknown --contracts mode %q (want off|panic|warn|stats)", mode)
	}
}

func cmdEmit(args []string) int {
	fs := flag.NewFlagSet("aril emit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noLine := fs.Bool("no-line", false, "strip //line directives from the lowered Go (for human reading)")
	vendor := fs.Bool("vendor-runtime", false, "emit `import .../arilrt` + arilrt.X instead of inlining the runtime (not self-contained)")
	contracts := addContractsFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril emit [-no-line] [-vendor-runtime] [-contracts=<mode>] <file.aril | dir>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := checkContractsMode(*contracts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "aril emit: expected exactly one <file.aril>")
		return 2
	}
	goSrc, err := emitGoSourceOpts(fs.Arg(0), *noLine, *vendor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(goSrc)
	return 0
}

// cmdImport generates a Aril foreign-binding file from a Go package's
// type info and prints it to stdout (ffi.md). The output is a curated
// starting point — unbindable symbols and guessed lifts are marked.
func cmdImport(args []string) int {
	fs := flag.NewFlagSet("aril import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril import <go/import/path>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "aril import: expected exactly one <go/import/path>")
		return 2
	}
	src, err := bindgen.Generate(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(src)
	if !bindgen.HasBindings(src) {
		fmt.Fprintf(os.Stderr, "aril import: %s has no bindable symbols (every export bailed)\n", fs.Arg(0))
	}
	return 0
}

func cmdBuild(args []string) int {
	fs := flag.NewFlagSet("aril build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("o", "", "output binary path (default: ./<basename>)")
	inlineRT := fs.Bool("inline-runtime", false, "inline the runtime into the single main.go instead of vendoring the arilrt package")
	contracts := addContractsFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril build [-o <path>] [-inline-runtime] [-contracts=<mode>] <file.aril | dir>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := checkContractsMode(*contracts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "aril build: expected exactly one <file.aril>")
		return 2
	}
	srcPath := fs.Arg(0)
	src, err := compileToTempGo(srcPath, !*inlineRT)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(src.dir)

	outPath := *out
	if outPath == "" {
		outPath = outputBinaryName(srcPath)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aril build: %v\n", err)
		return 1
	}
	// Build the root main package (".") rather than "./..." — vendored
	// mode adds the arilrt subpackage to the module, and `go build -o
	// <file> ./...` refuses to write multiple packages to one output.
	// arilrt is pulled in as main's dependency either way.
	cmd := exec.Command("go", "build", "-o", absOut, ".")
	cmd.Dir = src.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "aril build: go build failed: %v\n", err)
		return 1
	}
	return 0
}

// gatherSources resolves a build target to its set of `.aril` source
// files. A file path yields itself; a directory yields every `.aril` file
// in it (non-recursive), sorted for deterministic output — the whole
// directory is one package (RFC-0002 §"Package = directory").
func gatherSources(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("aril: cannot stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("aril: cannot read directory %s: %w", path, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".aril") {
			continue
		}
		files = append(files, filepath.Join(path, e.Name()))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("aril: no .aril files in %s", path)
	}
	sort.Strings(files)
	return files, nil
}

// emitGoSource lexes / parses / lowers the build target and returns the
// generated Go source string. Used by cmdEmit and (indirectly via
// compileToTempGo) by build / run.
func emitGoSource(path string) (string, error) {
	return emitGoSourceOpts(path, false, false)
}

// emitGoSourceOpts is the variant that takes the `no-line` flag —
// when true, the //line directives mapping back to the .aril source
// are suppressed (useful for reading the lowered Go directly).
// Build / run keep them on so panic traces and `go vet` errors
// still point at Aril source coordinates. `emit` does not require a
// `func main` (it lowers any package for inspection); build / run do.
func emitGoSourceOpts(path string, stripLine, vendored bool) (string, error) {
	files, userImports, err := buildUnit(path)
	if err != nil {
		return "", err
	}
	return compilePackage(files, userImports, stripLine, false, vendored)
}

// buildUnit resolves a build target into its full source-file set. It
// walks up for a aril.toml; with one, `import myproj/pkg` pulls the
// imported user package's .aril files into the build (RFC-0002
// §Resolution). Without a manifest the target is a lone package. Returns
// the file set and the set of user-package import paths (which codegen
// strips — they are satisfied by merged sources, not a Go import).
func buildUnit(path string) ([]string, map[string]bool, error) {
	files, err := gatherSources(path)
	if err != nil {
		return nil, nil, err
	}
	anchor := path
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		anchor = filepath.Dir(path)
	}
	m, err := findProjectManifest(anchor)
	if err != nil {
		return nil, nil, err
	}
	if m == nil {
		return files, nil, nil
	}
	res, err := resolvePackages(files, m)
	if err != nil {
		return nil, nil, err
	}
	return res.files, res.userImports, nil
}

// compilePackage lexes / parses / sema-checks / lowers a whole package
// (one or more `.aril` files sharing a Go `package main`). Diagnostics use
// each file's real path; //line labels are suppressed when stripLine is
// set. requireMain enforces exactly one `func main` across the package
// (RFC-0002) — on for build / run, off for emit.
func compilePackage(paths []string, userImports map[string]bool, stripLine, requireMain, vendored bool) (string, error) {
	trees := make([]*ast.File, len(paths))
	labels := make([]string, len(paths))
	for i, p := range paths {
		srcBytes, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("aril: cannot read %s: %w", p, err)
		}
		label := p
		if stripLine {
			label = ""
		}
		labels[i] = label
		toks, lerr := lexer.LexFile(string(srcBytes), label)
		if lerr != nil {
			return "", lerr
		}
		tree, perr := parser.ParseFile(toks, label)
		if perr != nil {
			return "", perr
		}
		// A user-package import is satisfied by merging that package's
		// sources into this build, not by a Go import — drop it so
		// codegen does not emit a dangling Go `import` (RFC-0002
		// §Resolution). Stdlib imports are kept.
		if len(userImports) > 0 {
			kept := tree.Imports[:0]
			for _, im := range tree.Imports {
				if !userImports[im.Path] {
					kept = append(kept, im)
				}
			}
			tree.Imports = kept
		}
		trees[i] = tree
	}
	info, diags := sema.CheckFiles(trees, labels)
	if len(diags) > 0 {
		for _, d := range diags {
			fmt.Fprintln(os.Stderr, d.Error())
		}
		return "", fmt.Errorf("aril: sema failed")
	}
	if requireMain {
		if err := checkPackageMain(trees, paths); err != nil {
			return "", err
		}
	}
	opts := codegen.Options{}
	if vendored {
		opts.Vendored = true
		opts.RuntimeImportPath = runtimeImportPath
	}
	goSrc, err := codegen.EmitFilesWithOptions(trees, labels, info, opts)
	if err != nil {
		return "", fmt.Errorf("aril: %s", err)
	}
	return goSrc, nil
}

// checkPackageMain enforces RFC-0002's build-entry rule: a package built
// to a binary must have exactly one `func main`. Reported in Aril terms
// (D10) rather than leaking the Go toolchain's "no main"/"redeclared"
// error from the merged output.
func checkPackageMain(trees []*ast.File, paths []string) error {
	count := 0
	for _, t := range trees {
		for _, d := range t.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name == "main" {
				count++
			}
		}
	}
	switch {
	case count == 0:
		return fmt.Errorf("aril: no `func main` in package %s", filepath.Dir(paths[0]))
	case count > 1:
		return fmt.Errorf("aril: package %s has %d `func main` declarations — exactly one is required", filepath.Dir(paths[0]), count)
	}
	return nil
}

// emitGoFromText runs the lexer / parser / codegen pipeline over
// an in-memory string. Used by REPL execution where the source
// is synthesised between turns rather than read from disk.
func emitGoFromText(src, file string) (string, error) {
	// Pass the path verbatim into diagnostics and //line
	// directives. test-contract.md §File paths requires
	// repo-relative paths so two files with the same basename
	// (e.g. a shared entry filename under different example dirs)
	// remain distinguishable in panic traces and diagnostics.
	toks, lerr := lexer.LexFile(src, file)
	if lerr != nil {
		return "", lerr
	}
	tree, perr := parser.ParseFile(toks, file)
	if perr != nil {
		return "", perr
	}
	info, diags := sema.Check(tree, file)
	if len(diags) > 0 {
		// Report all diags; return the first as the error so
		// callers stop, after printing the full batch.
		for _, d := range diags {
			fmt.Fprintln(os.Stderr, d.Error())
		}
		return "", fmt.Errorf("aril: sema failed")
	}
	goSrc, err := codegen.EmitWithInfo(tree, file, info)
	if err != nil {
		return "", fmt.Errorf("aril: %s", err)
	}
	return goSrc, nil
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("aril run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	inlineRT := fs.Bool("inline-runtime", false, "inline the runtime into the single main.go instead of vendoring the arilrt package")
	contracts := addContractsFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril run [-inline-runtime] [-contracts=<mode>] <file.aril | dir>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := checkContractsMode(*contracts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "aril run: expected exactly one <file.aril>")
		return 2
	}
	src, err := compileToTempGo(fs.Arg(0), !*inlineRT)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(src.dir)

	// Run the root main package (".") not "./..." — vendored mode adds
	// the arilrt subpackage and `go run ./...` would try to run multiple
	// packages. arilrt is compiled as main's dependency.
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = src.dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "aril run: go run failed: %v\n", err)
		return 1
	}
	return 0
}

// compiledSource bundles the path to a temporary directory that
// contains main.go + go.mod ready for `go build` / `go run`.
type compiledSource struct {
	dir string // caller is responsible for RemoveAll
}

// compileToTempGo lexes / parses / lowers the given build target (a
// file or a package directory) and writes main.go + go.mod into a fresh
// temp dir. The caller must RemoveAll the returned dir. Unlike emit, a
// runnable build requires exactly one `func main` (RFC-0002).
func compileToTempGo(path string, vendored bool) (*compiledSource, error) {
	files, userImports, err := buildUnit(path)
	if err != nil {
		return nil, err
	}
	goSrc, err := compilePackage(files, userImports, false, true, vendored)
	if err != nil {
		return nil, err
	}
	return writeTempModule(goSrc)
}

// compileSourceToTempGo is the in-memory variant used by the
// REPL: takes Aril source text + a synthetic file label for
// diagnostics, returns a runnable temp module.
func compileSourceToTempGo(src, label string) (*compiledSource, error) {
	goSrc, err := emitGoFromText(src, label)
	if err != nil {
		return nil, err
	}
	return writeTempModule(goSrc)
}

func writeTempModule(goSrc string) (*compiledSource, error) {
	// Resolve any third-party FFI bindings the program uses into a
	// go.mod with hermetic require + replace (ffi.md §"Dependency
	// model"); a stdlib-only program gets the plain require-free module.
	goMod, err := thirdPartyGoMod(goSrc)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "aril-build-*")
	if err != nil {
		return nil, fmt.Errorf("aril: mkdir temp: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("aril: write main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("aril: write go.mod: %w", err)
	}
	// Vendored-mode programs import the arilrt runtime as a subpackage of
	// the build module; copy the embedded sources into <dir>/arilrt so
	// `go build/run ./...` resolves it (Block R, D18 CT2 — the compiler
	// binary carries the exact runtime it emits). A program that emitted
	// inline (no arilrt import — e.g. reflection fell back to inline) gets
	// no subpackage. Keyed on the actual import so the two stay in step.
	if strings.Contains(goSrc, `"`+runtimeImportPath+`"`) {
		if _, err := aril.WriteVendoredRuntime(dir); err != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("aril: vendor runtime: %w", err)
		}
	}
	return &compiledSource{dir: dir}, nil
}

// outputBinaryName turns "examples/core-language/hello/hello.aril" → "./hello".
func outputBinaryName(path string) string {
	base := filepath.Base(path)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return filepath.Join(".", base)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `Aril - modern TypeScript-style syntax on the Go runtime.

Usage:
  aril <command> [arguments]

Commands:
  emit   [-no-line] <file.aril>  print the lowered Go source to stdout
  build  [-o out] <file.aril>    compile to a native binary (default: ./<basename>)
  run    <file.aril>             compile and execute (stdio passed through)
  repl                         interactive prompt (RFC-0003 skeleton)
  import <go/import/path>      generate Aril foreign bindings from a Go package
  version                      print the compiler version
  help                         print this message

Status: pre-alpha.`)
}
