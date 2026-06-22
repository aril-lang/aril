package sema

import "github.com/aril-lang/aril/internal/ast"

// Info is the AST-keyed side table. See docs/internals/sema.md §2.
type Info struct {
	// Symbol resolves *ast.Ident / *ast.NamedType to their Symbol.
	// Keyed by ast.Node — pointer identity into the AST is the
	// contract; non-pointer keys are not admitted.
	Symbol map[ast.Node]*Symbol

	// Type carries the inferred type of every expression Barrier C
	// visits. Keyed by the *ast.Expr node. Populated during body
	// checking; an expression sema could not type yet maps to a
	// *Unknown (the conservative wildcard).
	Type map[ast.Expr]Type

	// Def back-references a binding-introducing node (Param,
	// ClassField, VarStmt, IdentPat) to the Symbol it introduces.
	// Use-site idents already reach their Symbol through Symbol[];
	// Def closes the loop from the *declaration* side so Barrier C
	// can hang an inferred type on a let/var binding and codegen
	// can later read binding metadata without a parallel tracker.
	Def map[ast.Node]*Symbol

	// LoopInvariants maps a labelled loop (*ast.ForStmt / *ast.WhileStmt)
	// to the resolved, type-checked invariant predicates its matching
	// `contract … { loop <label> { invariant … } }` section attaches
	// (RFC-0006). Codegen lowers each to a per-iteration check. Absent for
	// a loop with no contract.
	LoopInvariants map[ast.Stmt][]ast.Expr
}

func newInfo() *Info {
	return &Info{
		Symbol:         map[ast.Node]*Symbol{},
		Type:           map[ast.Expr]Type{},
		Def:            map[ast.Node]*Symbol{},
		LoopInvariants: map[ast.Stmt][]ast.Expr{},
	}
}
