package parser

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// parseExternDecl parses a foreign-binding declaration (ffi.md):
//
//	ExternDecl = "extern" ( ExternType | ExternFunc | ExternImpl )
//
// `type` / `func` are keywords; `impl` is contextual — special only
// after `extern`, an ordinary identifier elsewhere.
func (p *parser) parseExternDecl() (ast.Decl, *Diag) {
	kw := p.advance() // consume 'extern'
	switch {
	case p.at(lexer.KindKeyword, "type"):
		return p.parseExternType(kw)
	case p.at(lexer.KindKeyword, "func"):
		return p.parseExternFunc(kw)
	case p.at(lexer.KindIdent, "impl"):
		return p.parseExternImpl(kw)
	}
	t := p.peek()
	return nil, p.diag("E0112",
		fmt.Sprintf("expected `type`, `func`, or `impl` after `extern`, got %s %q",
			t.Kind, t.Lexeme),
		t.Line, t.Col)
}

// parseExternType parses `extern type Ident GoAttr?` — an opaque handle.
func (p *parser) parseExternType(kw lexer.Token) (*ast.ExternTypeDecl, *Diag) {
	p.advance() // consume 'type'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	endLine, endCol := nameTok.Line, nameTok.Col+len(nameTok.Lexeme)
	goRef, err := p.parseGoAttrOpt()
	if err != nil {
		return nil, err
	}
	if goRef != nil {
		endLine, endCol = goRef.Span.EndLine, goRef.Span.EndCol
	}
	return &ast.ExternTypeDecl{
		Span: ast.Span{StartLine: kw.Line, StartCol: kw.Col, EndLine: endLine, EndCol: endCol},
		Name: nameTok.Lexeme,
		Go:   goRef,
	}, nil
}

// parseExternFunc parses
// `extern func Ident TypeParams? "(" Params? ")" ReturnType? GoAttr?`.
func (p *parser) parseExternFunc(kw lexer.Token) (*ast.ExternFuncDecl, *Diag) {
	p.advance() // consume 'func'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	typeParams, err := p.parseTypeParamList()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindPunct, "("); err != nil {
		return nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	closeTok, err := p.expect(lexer.KindPunct, ")")
	if err != nil {
		return nil, err
	}
	endLine, endCol := closeTok.Line, closeTok.Col+1
	var retType ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance()
		p.skipNewlines() // ReturnAnnot: type may wrap (grammar §SyntaxNewlineSuppression)
		retType, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		endLine, endCol = retType.NodeSpan().EndLine, retType.NodeSpan().EndCol
	}
	goRef, err := p.parseGoAttrOpt()
	if err != nil {
		return nil, err
	}
	if goRef != nil {
		endLine, endCol = goRef.Span.EndLine, goRef.Span.EndCol
	}
	return &ast.ExternFuncDecl{
		Span:       ast.Span{StartLine: kw.Line, StartCol: kw.Col, EndLine: endLine, EndCol: endCol},
		Name:       nameTok.Lexeme,
		TypeParams: typeParams,
		Params:     params,
		ReturnType: retType,
		Go:         goRef,
	}, nil
}

// parseExternImpl parses `extern impl Ident "{" ExternMember* "}"` where a
// member is `let|var name: T GoAttr?` (field) or `name(params): R? GoAttr?`
// (method); the parser commits on the leading `let`/`var` keyword.
func (p *parser) parseExternImpl(kw lexer.Token) (*ast.ExternImplDecl, *Diag) {
	p.advance() // consume contextual 'impl'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var methods []*ast.ExternMethod
	var fields []*ast.ExternField
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if p.at(lexer.KindKeyword, "let") || p.at(lexer.KindKeyword, "var") {
			f, err := p.parseExternField()
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
		} else {
			m, err := p.parseExternMethod()
			if err != nil {
				return nil, err
			}
			methods = append(methods, m)
		}
		p.skipNewlines()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	return &ast.ExternImplDecl{
		Span: ast.Span{StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1},
		Type:    nameTok.Lexeme,
		Methods: methods,
		Fields:  fields,
	}, nil
}

// parseExternMethod parses `name "(" Params? ")" ReturnType? GoAttr?`.
func (p *parser) parseExternMethod() (*ast.ExternMethod, *Diag) {
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindPunct, "("); err != nil {
		return nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	closeTok, err := p.expect(lexer.KindPunct, ")")
	if err != nil {
		return nil, err
	}
	endLine, endCol := closeTok.Line, closeTok.Col+1
	var retType ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance()
		p.skipNewlines()
		retType, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		endLine, endCol = retType.NodeSpan().EndLine, retType.NodeSpan().EndCol
	}
	goRef, err := p.parseGoAttrOpt()
	if err != nil {
		return nil, err
	}
	if goRef != nil {
		endLine, endCol = goRef.Span.EndLine, goRef.Span.EndCol
	}
	return &ast.ExternMethod{
		Span: ast.Span{StartLine: nameTok.Line, StartCol: nameTok.Col,
			EndLine: endLine, EndCol: endCol},
		Name:       nameTok.Lexeme,
		Params:     params,
		ReturnType: retType,
		Go:         goRef,
	}, nil
}

// parseExternField parses `let|var name ":" TypeExpr GoAttr?`.
func (p *parser) parseExternField() (*ast.ExternField, *Diag) {
	kw := p.advance() // 'let' or 'var'
	mut := "Let"
	if kw.Lexeme == "var" {
		mut = "Var"
	}
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindPunct, ":"); err != nil {
		return nil, err
	}
	ty, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	endLine, endCol := ty.NodeSpan().EndLine, ty.NodeSpan().EndCol
	goRef, err := p.parseGoAttrOpt()
	if err != nil {
		return nil, err
	}
	if goRef != nil {
		endLine, endCol = goRef.Span.EndLine, goRef.Span.EndCol
	}
	return &ast.ExternField{
		Span: ast.Span{StartLine: kw.Line, StartCol: kw.Col,
			EndLine: endLine, EndCol: endCol},
		Name:       nameTok.Lexeme,
		DeclType:   ty,
		Mutability: mut,
		Go:         goRef,
	}, nil
}

// parseGoAttrOpt parses an optional `@go("...")` foreign-binding attribute
// (ffi.md §GoRef). `@` is a token no ordinary production accepts; this is
// the FFI carve-out that cashes that reservation.
func (p *parser) parseGoAttrOpt() (*ast.GoRef, *Diag) {
	if !p.at(lexer.KindPunct, "@") {
		return nil, nil
	}
	at := p.advance() // consume '@'
	if !p.at(lexer.KindIdent, "go") {
		t := p.peek()
		return nil, p.diag("E0112",
			fmt.Sprintf("expected `go(...)` foreign-binding attribute after `@`, got %s %q",
				t.Kind, t.Lexeme),
			t.Line, t.Col)
	}
	p.advance() // consume 'go'
	if _, err := p.expect(lexer.KindPunct, "("); err != nil {
		return nil, err
	}
	strTok, err := p.expect(lexer.KindStringLit, "")
	if err != nil {
		return nil, err
	}
	raw, derr := decodeStringLit(strTok.Lexeme)
	if derr != nil {
		return nil, p.diag("E0112",
			fmt.Sprintf("invalid string in `@go` attribute: %v", derr),
			strTok.Line, strTok.Col)
	}
	closeTok, err := p.expect(lexer.KindPunct, ")")
	if err != nil {
		return nil, err
	}
	return &ast.GoRef{
		Span: ast.Span{StartLine: at.Line, StartCol: at.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1},
		Raw: raw,
	}, nil
}
