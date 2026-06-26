package sema

import (
	"github.com/aril-lang/aril/internal/ast"
)

// Check runs sema passes against f and returns the side-table
// plus any diagnostics, ordered by source position.
// See docs/internals/sema.md.
func Check(f *ast.File, file string) (*Info, []*Diag) {
	return CheckFiles([]*ast.File{f}, []string{file})
}

// CheckFiles runs sema over a whole package — every `.aril` file in a
// directory shares one top-level scope (RFC-0002 §"Package =
// directory"). The phases run file-by-file over the shared scope, with
// the per-file path tracked so each diagnostic carries its own source
// file. `paths[i]` is the source path of `files[i]`.
func CheckFiles(files []*ast.File, paths []string) (*Info, []*Diag) {
	c := &checker{
		info:          newInfo(),
		closureExpect: map[*ast.ClosureLit]*Func{},
		externImpls:   map[string]*ast.ExternImplDecl{},
	}
	scope := c.newPackageScope()
	for i, f := range files {
		c.file = paths[i]
		c.indexFile(f, scope)
	}
	// Contracts are indexed after all declarations are in the package
	// scope (so a contract can attach to a decl in any file) and before
	// resolution (so the resolve pass can bind loop-invariant predicates
	// at their loops).
	c.indexContracts(files, paths, scope)
	for i, f := range files {
		c.file = paths[i]
		c.resolveFile(f, scope)
	}
	for i, f := range files {
		c.file = paths[i]
		c.constructShapes(f, scope)
	}
	for i, f := range files {
		c.file = paths[i]
		c.checkBodies(f)
	}
	// Channel contracts (RFC-0007) are checked last: subject binding matches a
	// contract's named subjects against the inferred types of the program's
	// bindings, which are only known after body checking.
	c.checkChannelContracts(files, paths)
	sortDiags(c.diags)
	return c.info, c.diags
}

type checker struct {
	file  string
	info  *Info
	diags []*Diag

	// Per-body context, set before walking each function / method
	// body in Barrier C. v1 is single-threaded so plain fields are
	// safe; the parallel-per-body story (sema.md §8) would thread
	// these instead.
	curReturn       Type // declared return type of the body being checked
	curThis         Type // receiver type inside an instance method, else nil
	curTryForbidden bool // body returns a type that is definitely not Result/Option
	curSpawnFrame   bool // body is a spawn body — a Result<unit, error> frame (E0408)
	loopDepth       int  // enclosing for/while nesting — 0 ⇒ break/continue illegal (E0404)
	scopeDepth      int  // enclosing `scope` nesting — 0 ⇒ spawn illegal (E0405)

	// closureExpect carries the expected Func signature for a closure
	// passed as a call argument, so an unannotated short-closure
	// parameter is typed from call context (a comparator to
	// `sort.sorted`, etc.) rather than left Unknown. Keyed by the
	// closure node; set just before its enclosing argument is inferred.
	closureExpect map[*ast.ClosureLit]*Func

	// externImpls maps an opaque foreign handle's name to the
	// `extern impl T { … }` block carrying its methods/fields, so
	// member access on a handle (ffi.md §ExternImpl) can resolve.
	externImpls map[string]*ast.ExternImplDecl

	// contractByTarget maps a declaration name to its separable contract
	// block (RFC-0006); curContract is the contract of the function whose
	// body is being resolved/checked, or nil.
	contractByTarget map[string]*ast.ContractDecl
	curContract      *ast.ContractDecl
	// invariantTypes is the set of class/record type names carrying a
	// top-level `invariant` (RFC-0006). Populated at index time (before
	// bodies) so the E1106 external-field-write guard is order-independent
	// — an assignment may precede the invariant type's declaration.
	invariantTypes map[string]bool
	// matchedLoopLabels tracks which of curContract's loop-section labels
	// the resolve walk has matched against a real labelled loop, so an
	// unmatched section can be flagged (E1101). Reset per function.
	matchedLoopLabels map[string]bool
	// contractEntrySyms carries a function's `entry { let … }` binding
	// symbols from the resolve pass to the check pass (where their types
	// are set from the inferred binding values).
	contractEntrySyms map[*ast.FuncDecl][]*Symbol
	// protocolContracts collects the RFC-0007 cross-channel protocol contracts
	// (a `contract` block carrying protocol clauses) skipped by the value-
	// contract index — they attach to channels, not a value/state target, so
	// the channel-contract pass handles them after body checking.
	protocolContracts []contractAt
}

// contractAt pairs a contract declaration with its source path (protocol
// contracts are collected during indexing and checked in a later pass).
type contractAt struct {
	decl *ast.ContractDecl
	file string
}

func (c *checker) report(code, message string, span ast.Span) {
	c.diags = append(c.diags, &Diag{
		File:    c.file,
		Code:    code,
		Message: message,
		Line:    span.StartLine,
		Col:     span.StartCol,
	})
}
