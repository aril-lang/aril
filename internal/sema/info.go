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

	// FuncContracts maps a FuncDecl to its resolved, type-checked
	// requires/ensures/entry obligations (RFC-0006). Codegen lowers
	// `requires` to an entry check, `entry` bindings to entry temps, and
	// `ensures` to a deferred post-check. Absent for a function with no
	// contract (or whose contract carries only loop sections).
	FuncContracts map[*ast.FuncDecl]*FuncContract

	// TypeInvariants maps a type name (class or record) to the resolved,
	// type-checked top-level `invariant` predicates its
	// `contract <Type> { invariant … }` attaches (RFC-0006). The predicates
	// resolve in the type's field scope (bare field names → SymField).
	// Codegen lowers each to a check at every brace-literal construction
	// site, and — for a class — at every non-static method exit (the
	// mutation boundary). Keyed by name (unique per package, D27) so both
	// kinds and both checkpoints share one lookup. Absent for a type with no
	// invariant.
	TypeInvariants map[string][]ast.Expr
}

// FuncContract is a function's resolved requires/ensures/entry obligations.
type FuncContract struct {
	Requires []ast.Expr     // precondition predicates (param scope)
	Ensures  []ast.Expr     // postcondition predicates (params + entry + result)
	Entries  []EntryBinding // entry snapshots, in source order
}

// EntryBinding is one `entry { let Name = Value }` snapshot (RFC-0006).
type EntryBinding struct {
	Name  string
	Value ast.Expr
}

func newInfo() *Info {
	return &Info{
		Symbol:         map[ast.Node]*Symbol{},
		Type:           map[ast.Expr]Type{},
		Def:            map[ast.Node]*Symbol{},
		LoopInvariants: map[ast.Stmt][]ast.Expr{},
		FuncContracts:  map[*ast.FuncDecl]*FuncContract{},
		TypeInvariants: map[string][]ast.Expr{},
	}
}
