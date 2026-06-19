package codegen

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
)

// This file holds statement-level emission: block / function-body
// emission, the emitStmt dispatch, and the let / var / tuple-
// destructure lowering. Split out of the codegen.go god-file;
// behaviour-preserving.

func (g *gen) emitBlockBody(b *ast.Block) error {
	for _, s := range b.Stmts {
		if err := g.emitStmt(s); err != nil {
			return err
		}
	}
	if b.Trailing != nil {
		// Statement-context block: the trailing value is discarded.
		// Value-context blocks are lowered to an IIFE by
		// emitBlockAsExpr, which never calls this.
		return g.emitStmt(&ast.ExprStmt{Span: b.Trailing.NodeSpan(), Expr: b.Trailing})
	}
	return nil
}

// emitFuncBody lowers a function / method / closure body. Unlike a
// statement-position block, the trailing expression of a body whose
// result is a value is an *implicit return* (block-as-expression value
// rule; lowering-go.md §"Implicit tail return"): it is emitted in tail
// position (emitTailReturn) so a trailing match/if distributes the
// `return` into its leaves and the declared return type flows down for
// constructor type-arg stamping. A unit-returning body keeps the
// statement-position discard; a body with no trailing (ends in explicit
// `return`s) emits nothing extra. isUnit is passed explicitly because a
// closure's unit-ness can come from sema inference, not just a nil
// annotation.
func (g *gen) emitFuncBody(b *ast.Block, ret ast.TypeExpr, isUnit bool) error {
	for _, s := range b.Stmts {
		if err := g.emitStmt(s); err != nil {
			return err
		}
	}
	if b.Trailing == nil {
		return nil
	}
	if isUnit {
		return g.emitStmt(&ast.ExprStmt{Span: b.Trailing.NodeSpan(), Expr: b.Trailing})
	}
	prev := g.expectType
	g.expectType = ret
	err := g.emitTailReturn(b.Trailing)
	g.expectType = prev
	return err
}

// isUnitReturn reports whether a declared return type carries no Go
// value: a nil annotation (the implicit unit return) or an explicit
// `unit`.
func isUnitReturn(t ast.TypeExpr) bool {
	if t == nil {
		return true
	}
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "unit"
}

func (g *gen) emitStmt(s ast.Stmt) error {
	switch v := s.(type) {
	case *ast.ExprStmt:
		// ReturnExpr (DivergingExpr): lower to Go `return` stmt.
		if r, ok := v.Expr.(*ast.ReturnExpr); ok {
			// Inside a `spawn` body the func returns `error`, so a
			// `return Ok(())` / `return Err(e)` (Result<unit, E>) is
			// converted to the group's error channel (lowering-go.md
			// §SpawnIR).
			if g.inSpawnBody {
				return g.emitSpawnReturn(r)
			}
			// `return try e` — emit the try preamble, then
			// `return tmp.V`.
			if try, ok := r.Value.(*ast.TryExpr); ok {
				tmp, err := g.emitTryPreamble(try)
				if err != nil {
					return err
				}
				g.line(r.Span.StartLine)
				g.writeIndent()
				g.b.WriteString("return ")
				g.b.WriteString(tmp)
				g.b.WriteString(".V\n")
				return nil
			}
			if r.Value == nil {
				g.line(v.Span.StartLine)
				g.writeIndent()
				g.b.WriteString("return\n")
				return nil
			}
			// `return f(try g())` / `return a + try b()` — hoist the
			// nested try preambles before the `return` line.
			if err := g.hoistTriesIfSafe(r.Value); err != nil {
				return err
			}
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("return ")
			// The returned value is expected to be the function's
			// declared return type; thread it so a predeclared
			// Result/Option constructor gets explicit type args.
			prevExpect := g.expectType
			g.expectType = g.curFuncReturn
			err := g.emitExpr(r.Value)
			g.expectType = prevExpect
			if err != nil {
				return err
			}
			g.b.WriteByte('\n')
			return nil
		}
		// Bare `try e` as a discarded expression statement.
		if try, ok := v.Expr.(*ast.TryExpr); ok {
			_, err := g.emitTryPreamble(try)
			return err
		}
		// Diverging loop expressions lower to Go statements.
		if _, ok := v.Expr.(*ast.BreakExpr); ok {
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("break\n")
			return nil
		}
		if _, ok := v.Expr.(*ast.ContinueExpr); ok {
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("continue\n")
			return nil
		}
		// MatchExpr: lower to Go `switch` statement.
		if m, ok := v.Expr.(*ast.MatchExpr); ok {
			return g.emitMatchAsStmt(m)
		}
		// Block-as-expression in statement position: run the
		// statements inline, discarding the trailing value.
		if blk, ok := v.Expr.(*ast.Block); ok {
			return g.emitBlockBody(blk)
		}
		// IfExpr in statement position: same shape as an if-statement.
		if ie, ok := v.Expr.(*ast.IfExpr); ok {
			return g.emitIfExprAsStmt(ie)
		}
		// `spawn { … }` registers a goroutine on the enclosing
		// scope's group (lowering-go.md §SpawnIR).
		if sp, ok := v.Expr.(*ast.SpawnExpr); ok {
			return g.emitSpawnStmt(sp)
		}
		// Expression-statement (`stack.push(try …)`) — hoist any
		// nested `try` to preambles before emitting the call.
		if err := g.hoistTriesIfSafe(v.Expr); err != nil {
			return err
		}
		g.line(v.Span.StartLine)
		g.writeIndent()
		if err := g.emitExpr(v.Expr); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	case *ast.IfStmt:
		return g.emitIfStmt(v)
	case *ast.WhileStmt:
		return g.emitWhileStmt(v)
	case *ast.SelectStmt:
		return g.emitSelectStmt(v)
	case *ast.DeferStmt:
		// lowering-go.md §Defer: `defer call(args)` → Go `defer
		// call(args)` directly (G27 — adopted from Go).
		g.line(v.Span.StartLine)
		g.writeIndent()
		g.b.WriteString("defer ")
		if err := g.emitExpr(v.Call); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	case *ast.ForStmt:
		return g.emitForStmt(v)
	case *ast.LetStmt:
		switch pat := v.Pattern.(type) {
		case *ast.IdentPat:
			return g.emitLetOrVar(v.Span, pat.Name, v.DeclType, v.Value)
		case *ast.TuplePat:
			return g.emitDestructureLet(v.Span, pat, v.Value)
		default:
			return fmt.Errorf("codegen: unsupported `let` pattern %T", v.Pattern)
		}
	case *ast.VarStmt:
		return g.emitLetOrVar(v.Span, v.Name, v.DeclType, v.Value)
	case *ast.AssignStmt:
		// `total = total + try f()` / `m[try k()] = v` — hoist nested
		// try preambles before any of the assignment is emitted. LValue
		// then Value as one frame, so the order check spans both.
		if err := g.hoistTriesIfSafe(v.LValue, v.Value); err != nil {
			return err
		}
		g.line(v.Span.StartLine)
		g.writeIndent()
		// `m[k] = val` where m is a Map<K, V> lowers to
		// `m.set(k, val)` — the wrapper's set() updates both the
		// internal map and the insertion-order slice. Direct
		// `m.m[k] = val` would bypass that and break iteration
		// order for any later `.entries()`/`.keys()` call.
		if idx, ok := v.LValue.(*ast.Index); ok {
			if id, ok := idx.Receiver.(*ast.Ident); ok && g.varKindOf(id) == "Map" {
				if err := g.emitExpr(id); err != nil {
					return err
				}
				g.b.WriteString(".Set(")
				if err := g.emitExpr(idx.Idx); err != nil {
					return err
				}
				g.b.WriteString(", ")
				if err := g.emitExpr(v.Value); err != nil {
					return err
				}
				g.b.WriteString(")\n")
				return nil
			}
		}
		if err := g.emitExpr(v.LValue); err != nil {
			return err
		}
		g.b.WriteString(" = ")
		if err := g.emitExpr(v.Value); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	}
	return fmt.Errorf("codegen: unhandled stmt %T", s)
}

// emitDestructureLet lowers `let (a, b) = e` (lowering-go.md
// §Tuple destructuring). The value is bound to a fresh temp once (so a
// side-effecting RHS runs exactly once), then each component is bound
// positionally via bindSubPattern (`a := tmp._0`, …), recursing for
// nested tuples; a `_` component binds nothing. When every component is
// `_` the value is discarded (`_ = e`) so Go sees no unused temp.
func (g *gen) emitDestructureLet(span ast.Span, pat *ast.TuplePat, value ast.Expr) error {
	g.line(span.StartLine)
	g.writeIndent()
	if patternBindsNothing(pat) {
		g.b.WriteString("_ = ")
		if err := g.emitExpr(value); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	}
	tmp := g.nextDestructureTemp()
	g.b.WriteString(tmp)
	g.b.WriteString(" := ")
	if err := g.emitExpr(value); err != nil {
		return err
	}
	g.b.WriteByte('\n')
	return g.bindSubPattern(pat, tmp)
}

// patternBindsNothing reports whether an irrefutable let pattern
// introduces no binding at all (every leaf is `_`), so the temp would
// be unused — the value is discarded instead.
func patternBindsNothing(p ast.Pattern) bool {
	switch v := p.(type) {
	case *ast.WildcardPat:
		return true
	case *ast.TuplePat:
		for _, sub := range v.Sub {
			if !patternBindsNothing(sub) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// nextDestructureTemp returns a fresh Go identifier for a
// tuple-destructuring temp, sharing the runtime-prefix convention with
// the other codegen-internal temps.
func (g *gen) nextDestructureTemp() string {
	g.destructureTempCounter++
	return fmt.Sprintf("__aril_destructure_%d", g.destructureTempCounter)
}

// emitLetOrVar lowers both `let` and `var` to Go's `var name [T] = value`.
// Immutability of `let` is a sema concern (not yet implemented); the
// generated Go is identical for both keywords.
func (g *gen) emitLetOrVar(span ast.Span, name string, declType ast.TypeExpr, value ast.Expr) error {
	// `let x = try e` / `var x = try e` — emit the try
	// preamble, then bind the unwrapped value.
	if try, ok := value.(*ast.TryExpr); ok {
		tmp, err := g.emitTryPreamble(try)
		if err != nil {
			return err
		}
		g.line(span.StartLine)
		g.writeIndent()
		g.b.WriteString("var ")
		g.b.WriteString(goIdent(name))
		if declType != nil {
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(declType); err != nil {
				return err
			}
		}
		g.b.WriteString(" = ")
		g.b.WriteString(tmp)
		g.b.WriteString(".V\n")
		return nil
	}
	// `let x = f(try g())` / `let x = a + try b()` — hoist nested try
	// preambles before the binding line.
	if err := g.hoistTriesIfSafe(value); err != nil {
		return err
	}
	g.line(span.StartLine)
	g.writeIndent()
	g.b.WriteString("var ")
	g.b.WriteString(goIdent(name))
	if declType != nil {
		g.b.WriteByte(' ')
		if err := g.emitTypeExpr(declType); err != nil {
			return err
		}
	}
	g.b.WriteString(" = ")
	// A type annotation gives the value an expected type — thread it
	// so a predeclared Result/Option constructor gets explicit type
	// args (Go does not infer a generic call's type params from the
	// assignment LHS). nil annotation leaves inference unchanged.
	prevExpect := g.expectType
	g.expectType = declType
	err := g.emitExpr(value)
	g.expectType = prevExpect
	if err != nil {
		return err
	}
	g.b.WriteByte('\n')
	return nil
}
