package parser

import (
	"fmt"
	"unicode/utf8"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// parseTypeExpr emits PrimitiveType for the closed PrimitiveName
// set, SliceType for `[]T`, and NamedType for everything else.
// Form:
//
//	TypeExpr = PrimitiveType
//	         | NamedType
//	NamedType = Ident ("." Ident)*  ("<" TypeArgList ">")?
//
// Generic args are parsed if present (so `Result<int, error>` and
// `Map<string, int>` parse cleanly), even though the only
// type-bearing positions in PR-F1's corpus use bare primitives.
func (p *parser) parseTypeExpr() (ast.TypeExpr, *Diag) {
	// FuncType: `func(A, B) : R`.
	if p.at(lexer.KindKeyword, "func") {
		kw := p.advance() // consume 'func'
		if _, err := p.expect(lexer.KindPunct, "("); err != nil {
			return nil, err
		}
		var params []ast.TypeExpr
		p.skipNewlines()
		for !p.at(lexer.KindPunct, ")") && !p.at(lexer.KindEOF) {
			pt, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			params = append(params, pt)
			p.skipNewlines()
			if !p.at(lexer.KindPunct, ",") {
				break
			}
			p.advance()
			p.skipNewlines()
		}
		closeTok, err := p.expect(lexer.KindPunct, ")")
		if err != nil {
			return nil, err
		}
		ft := &ast.FuncType{
			Span: ast.Span{StartLine: kw.Line, StartCol: kw.Col,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1},
			Params: params,
		}
		if p.at(lexer.KindPunct, ":") {
			p.advance()
			p.skipNewlines() // ReturnAnnot: type may wrap to next line (grammar §SyntaxNewlineSuppression)
			rt, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			ft.ReturnType = rt
			ft.Span.EndLine, ft.Span.EndCol = rt.NodeSpan().EndLine, rt.NodeSpan().EndCol
		}
		return ft, nil
	}
	// TupleType: `(A, B, ...)` — arity ≥ 2.
	if p.at(lexer.KindPunct, "(") {
		open := p.advance() // consume '('
		p.skipNewlines()
		var comps []ast.TypeExpr
		for !p.at(lexer.KindPunct, ")") && !p.at(lexer.KindEOF) {
			c, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			comps = append(comps, c)
			p.skipNewlines()
			if !p.at(lexer.KindPunct, ",") {
				break
			}
			p.advance() // consume ','
			p.skipNewlines()
		}
		closeTok, err := p.expect(lexer.KindPunct, ")")
		if err != nil {
			return nil, err
		}
		// Arrow FuncType: `(A, B) => R` — the trailing `=>` disambiguates
		// the paren-list from a TupleType. The list is the parameter list
		// at any arity (incl. `()` and a single `(string)`), so the
		// arity-≥2 tuple check below applies only when there is no `=>`.
		// (grammar.ebnf §FuncType.) Type position only — no clash with the
		// expression-level short closure `(a, b) => expr`.
		if p.at(lexer.KindOp, "=>") {
			p.advance()
			p.skipNewlines() // return type may wrap (grammar §SyntaxNewlineSuppression)
			rt, rerr := p.parseTypeExpr()
			if rerr != nil {
				return nil, rerr
			}
			return &ast.FuncType{
				Span: ast.Span{
					StartLine: open.Line, StartCol: open.Col,
					EndLine: rt.NodeSpan().EndLine, EndCol: rt.NodeSpan().EndCol,
				},
				Params:     comps,
				ReturnType: rt,
			}, nil
		}
		if len(comps) < 2 {
			return nil, p.diag("E0112", "tuple type needs at least two components", open.Line, open.Col)
		}
		return &ast.TupleType{
			Span: ast.Span{
				StartLine: open.Line, StartCol: open.Col,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
			},
			Components: comps,
		}, nil
	}
	// SliceType: `[]T`.
	if p.at(lexer.KindPunct, "[") {
		openTok := p.advance() // consume '['
		if _, err := p.expect(lexer.KindPunct, "]"); err != nil {
			return nil, err
		}
		elem, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		return &ast.SliceType{
			Span: ast.Span{
				StartLine: openTok.Line, StartCol: openTok.Col,
				EndLine: elem.NodeSpan().EndLine, EndCol: elem.NodeSpan().EndCol,
			},
			Elem: elem,
		}, nil
	}
	// `unit` lexes as a keyword (it is also the value-type with the
	// one inhabitant `()`), so accept it as a PrimitiveType here —
	// the closed-set check below only sees KindIdent tokens.
	if p.at(lexer.KindKeyword, "unit") {
		t := p.advance()
		return &ast.PrimitiveType{
			Span: ast.Span{
				StartLine: t.Line, StartCol: t.Col,
				EndLine: t.Line, EndCol: t.Col + utf8.RuneCountInString(t.Lexeme),
			},
			Name: "unit",
		}, nil
	}
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return nil, p.diag("E0112",
			fmt.Sprintf("expected a type, got %s", describeToken(t)),
			t.Line, t.Col)
	}
	first := p.advance()
	startLine, startCol := first.Line, first.Col
	endLine, endCol := first.Line, first.Col+utf8.RuneCountInString(first.Lexeme)

	// Commit to PrimitiveType when the first segment is a member
	// of the closed primitive-name set AND there is no further
	// qualification (`.`) or type-arg list. `int.Foo` and
	// `Result<int>` continue to flow through the NamedType path.
	isPrim := ast.PrimitiveNames[first.Lexeme]
	if isPrim && !p.at(lexer.KindPunct, ".") && !p.at(lexer.KindOp, "<") {
		return &ast.PrimitiveType{
			Span: ast.Span{
				StartLine: startLine, StartCol: startCol,
				EndLine: endLine, EndCol: endCol,
			},
			Name: first.Lexeme,
		}, nil
	}

	qname := []string{first.Lexeme}
	for p.at(lexer.KindPunct, ".") {
		p.advance() // consume '.'
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112", "expected identifier after `.`", t.Line, t.Col)
		}
		next := p.advance()
		qname = append(qname, next.Lexeme)
		endLine, endCol = next.Line, next.Col+utf8.RuneCountInString(next.Lexeme)
	}
	var args []ast.TypeExpr
	if p.at(lexer.KindOp, "<") {
		p.advance() // consume '<'
		for {
			p.skipNewlines()
			arg, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			p.skipNewlines()
			if !p.at(lexer.KindPunct, ",") {
				break
			}
			p.advance() // consume ','
		}
		closeTok, err := p.expect(lexer.KindOp, ">")
		if err != nil {
			return nil, err
		}
		endLine, endCol = closeTok.Line, closeTok.Col+1
	}
	// `Result<T>` defaults its error type to `error` (desugaring.md
	// §Result-Default): normalize the one-arg form to `Result<T, error>`
	// so every downstream consumer (sema resolution, arity check, codegen
	// lowering) sees the full two-arg shape uniformly. The synthetic
	// `error` arg carries the whole instantiation's span.
	if len(qname) == 1 && qname[0] == "Result" && len(args) == 1 {
		args = append(args, &ast.NamedType{
			Span: ast.Span{
				StartLine: startLine, StartCol: startCol,
				EndLine: endLine, EndCol: endCol,
			},
			QName: []string{"error"},
		})
	}
	return &ast.NamedType{
		Span: ast.Span{
			StartLine: startLine, StartCol: startCol,
			EndLine: endLine, EndCol: endCol,
		},
		QName: qname,
		Args:  args,
	}, nil
}
