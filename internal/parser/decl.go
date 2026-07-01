package parser

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

func (p *parser) parseImport() (*ast.Import, *Diag) {
	kw := p.advance() // consume 'import'
	// Path is one-or-more identifiers separated by `/` per
	// grammar.ebnf PackagePath = Ident ("/" Ident)*. Dots are not
	// admitted (member access on a package is the field operator
	// `.`, not part of the import path).
	var parts []string
	end := kw
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return nil, p.diag("E0112", "expected identifier after `import`", t.Line, t.Col)
	}
	t := p.advance()
	parts = append(parts, t.Lexeme)
	end = t
	for p.at(lexer.KindOp, "/") {
		sep := p.advance()
		parts = append(parts, sep.Lexeme)
		if !p.at(lexer.KindIdent) {
			t = p.peek()
			return nil, p.diag("E0112", "expected identifier in import path", t.Line, t.Col)
		}
		next := p.advance()
		parts = append(parts, next.Lexeme)
		end = next
	}
	return &ast.Import{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: end.Line, EndCol: end.Col + utf8.RuneCountInString(end.Lexeme),
		},
		Path: strings.Join(parts, ""),
	}, nil
}

func (p *parser) parseDecl() (ast.Decl, *Diag) {
	if p.at(lexer.KindKeyword, "func") {
		return p.parseFuncDecl()
	}
	if p.at(lexer.KindKeyword, "type") {
		return p.parseTypeDecl()
	}
	if p.at(lexer.KindKeyword, "class") {
		return p.parseClassDecl()
	}
	if p.at(lexer.KindKeyword, "interface") {
		return p.parseInterfaceDecl()
	}
	if p.at(lexer.KindKeyword, "let") {
		return p.parseTopLevelLet()
	}
	if p.at(lexer.KindKeyword, "extern") {
		return p.parseExternDecl()
	}
	t := p.peek()
	return nil, p.diag("E0112",
		fmt.Sprintf("expected top-level declaration, got %s %q", t.Kind, t.Lexeme),
		t.Line, t.Col)
}

// parseTopLevelLet parses a module-level constant
// `let Ident (":" TypeExpr)? "=" Expr` (grammar.ebnf §TopLevelLet).
// The initialiser is mandatory; `var` is intentionally not legal at
// the top level (it falls through to the E0112 arm above).
func (p *parser) parseTopLevelLet() (*ast.TopLevelLet, *Diag) {
	kw := p.advance() // consume 'let'
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return nil, p.diag("E0112", "expected binding name", t.Line, t.Col)
	}
	nameTok := p.advance()
	var declType ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance() // consume ':'
		var err *Diag
		declType, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(lexer.KindOp, "="); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.TopLevelLet{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: value.NodeSpan().EndLine, EndCol: value.NodeSpan().EndCol,
		},
		Name:     nameTok.Lexeme,
		DeclType: declType,
		Value:    value,
	}, nil
}

// parseTypeDecl parses `type Name = TypeBody`. PR-F2 supports
// SumTypeBody (nullary variants) and AliasBody. RecordTypeBody
// and TupleAliasBody land with later PRs.
func (p *parser) parseTypeDecl() (*ast.TypeDecl, *Diag) {
	kw := p.advance() // consume 'type'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	// Optional type parameters — `type Pair<T, U> = …`
	// (grammar.ebnf §TypeDecl). Shares parseTypeParamList with
	// class / func / interface decls.
	typeParams, err := p.parseTypeParamList()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindOp, "="); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var body ast.TypeBody
	// SumTypeBody iff the body starts with `|`; RecordTypeBody iff
	// it starts with `{`.
	if p.at(lexer.KindOp, "|") {
		sb, err := p.parseSumTypeBody()
		if err != nil {
			return nil, err
		}
		body = sb
	} else if p.at(lexer.KindPunct, "{") {
		rb, err := p.parseRecordTypeBody()
		if err != nil {
			return nil, err
		}
		body = rb
	} else {
		// AliasBody — single TypeExpr.
		startLine, startCol := p.peek().Line, p.peek().Col
		ty, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		body = &ast.AliasBody{
			Span: ast.Span{
				StartLine: startLine, StartCol: startCol,
				EndLine: ty.NodeSpan().EndLine, EndCol: ty.NodeSpan().EndCol,
			},
			Aliased: ty,
		}
	}
	return &ast.TypeDecl{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.NodeSpan().EndLine, EndCol: body.NodeSpan().EndCol,
		},
		Name:       nameTok.Lexeme,
		TypeParams: typeParams,
		Body:       body,
	}, nil
}

// parseSumTypeBody expects the cursor at the leading `|`.
func (p *parser) parseSumTypeBody() (*ast.SumTypeBody, *Diag) {
	startTok := p.peek()
	var variants []*ast.Variant
	for p.at(lexer.KindOp, "|") {
		p.advance() // consume '|'
		p.skipNewlines()
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112", "expected variant name after `|`", t.Line, t.Col)
		}
		vnTok := p.advance()
		v := &ast.Variant{
			Span: ast.Span{
				StartLine: vnTok.Line, StartCol: vnTok.Col,
				EndLine: vnTok.Line, EndCol: vnTok.Col + utf8.RuneCountInString(vnTok.Lexeme),
			},
			Name: vnTok.Lexeme,
		}
		// Optional payload: `(name: T, name: T, ...)`.
		if p.at(lexer.KindPunct, "(") {
			p.advance() // consume '('
			p.skipNewlines()
			for !p.at(lexer.KindPunct, ")") {
				if !p.at(lexer.KindIdent) {
					t := p.peek()
					return nil, p.diag("E0112", "expected field name in variant payload", t.Line, t.Col)
				}
				fnTok := p.advance()
				if _, err := p.expect(lexer.KindPunct, ":"); err != nil {
					return nil, err
				}
				ft, err := p.parseTypeExpr()
				if err != nil {
					return nil, err
				}
				v.Fields = append(v.Fields, &ast.FieldDecl{
					Span: ast.Span{
						StartLine: fnTok.Line, StartCol: fnTok.Col,
						EndLine: ft.NodeSpan().EndLine, EndCol: ft.NodeSpan().EndCol,
					},
					Name:     fnTok.Lexeme,
					DeclType: ft,
				})
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
			v.Span.EndLine = closeTok.Line
			v.Span.EndCol = closeTok.Col + 1
		}
		variants = append(variants, v)
		p.skipNewlines()
	}
	if len(variants) < 2 {
		// ast.md:111 — SumTypeBody requires variants.len() >= 2.
		// A single-variant "sum" should be a class or a struct.
		return nil, p.diag("E0112", "sum type must have at least two variants", startTok.Line, startTok.Col)
	}
	last := variants[len(variants)-1]
	return &ast.SumTypeBody{
		Span: ast.Span{
			StartLine: startTok.Line, StartCol: startTok.Col,
			EndLine: last.Span.EndLine, EndCol: last.Span.EndCol,
		},
		Variants: variants,
	}, nil
}

// parseRecordTypeBody parses `{ f1: T1, f2: T2, ... }` (grammar.ebnf
// RecordType). Fields are separated by commas and/or newlines; a
// trailing separator is allowed. Cursor at the opening `{`.
func (p *parser) parseRecordTypeBody() (*ast.RecordTypeBody, *Diag) {
	open, err := p.expect(lexer.KindPunct, "{")
	if err != nil {
		return nil, err
	}
	p.skipStmtSeps()
	var fields []*ast.FieldDecl
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112", "expected record field name", t.Line, t.Col)
		}
		nameTok := p.advance()
		if _, err := p.expect(lexer.KindPunct, ":"); err != nil {
			return nil, err
		}
		ft, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, &ast.FieldDecl{
			Span: ast.Span{
				StartLine: nameTok.Line, StartCol: nameTok.Col,
				EndLine: ft.NodeSpan().EndLine, EndCol: ft.NodeSpan().EndCol,
			},
			Name:     nameTok.Lexeme,
			DeclType: ft,
		})
		// Field separator: a comma and/or newlines, or the closing `}`.
		if p.at(lexer.KindPunct, ",") {
			p.advance()
		}
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, p.diag("E0112", "record type needs at least one field", open.Line, open.Col)
	}
	return &ast.RecordTypeBody{
		Span: ast.Span{
			StartLine: open.Line, StartCol: open.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Fields: fields,
	}, nil
}

// parseClassDecl parses `class Name<TypeParams> { fields, methods }`.
// Type parameters arrived with PR-G1 (the `<T, U>` list after the
// name is admitted; bare names only, constraints come with PR-G3).
// `implements` is still rejected at parse time and lands with the
// interface PR. A class member is either `let|var name: T` (field)
// or `[static] name<TypeParams>?(params)? body` (method); the
// parser commits based on the leading keyword.
func (p *parser) parseClassDecl() (*ast.ClassDecl, *Diag) {
	kw := p.advance() // consume 'class'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	typeParams, err := p.parseTypeParamList()
	if err != nil {
		return nil, err
	}
	var implements []ast.TypeExpr
	if p.at(lexer.KindKeyword, "implements") {
		p.advance()
		impl, err := p.parseInterfaceList()
		if err != nil {
			return nil, err
		}
		implements = impl
	}
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var fields []*ast.ClassField
	var methods []*ast.Method
	for !p.at(lexer.KindPunct, "}") {
		if p.at(lexer.KindKeyword, "let") || p.at(lexer.KindKeyword, "var") {
			f, err := p.parseClassField()
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
		} else {
			m, err := p.parseMethod()
			if err != nil {
				return nil, err
			}
			methods = append(methods, m)
		}
		p.skipNewlines()
	}
	closeTok := p.advance() // consume '}'
	return &ast.ClassDecl{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Name:       nameTok.Lexeme,
		TypeParams: typeParams,
		Implements: implements,
		Fields:     fields,
		Methods:    methods,
	}, nil
}

// parseInterfaceList parses `TypeExpr ( "," TypeExpr )*` — the
// `implements` / `extends` conformance list.
func (p *parser) parseInterfaceList() ([]ast.TypeExpr, *Diag) {
	var out []ast.TypeExpr
	for {
		t, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		if !p.at(lexer.KindPunct, ",") {
			break
		}
		p.advance()
		p.skipNewlines()
	}
	return out, nil
}

// parseInterfaceDecl parses `interface Name<T> (extends List)? {
// methodSig* }` (grammar.ebnf InterfaceDecl).
func (p *parser) parseInterfaceDecl() (*ast.InterfaceDecl, *Diag) {
	kw := p.advance() // consume 'interface'
	nameTok, err := p.expect(lexer.KindIdent, "")
	if err != nil {
		return nil, err
	}
	typeParams, err := p.parseTypeParamList()
	if err != nil {
		return nil, err
	}
	var extends []ast.TypeExpr
	if p.at(lexer.KindKeyword, "extends") {
		p.advance()
		extends, err = p.parseInterfaceList()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var methods []*ast.InterfaceMethodSig
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		mnameTok, err := p.expect(lexer.KindIdent, "")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.KindPunct, "("); err != nil {
			return nil, err
		}
		var params []*ast.Param
		if !p.at(lexer.KindPunct, ")") {
			params, err = p.parseParamList()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(lexer.KindPunct, ")"); err != nil {
			return nil, err
		}
		sig := &ast.InterfaceMethodSig{
			Span: ast.Span{StartLine: mnameTok.Line, StartCol: mnameTok.Col,
				EndLine: mnameTok.Line, EndCol: mnameTok.Col + len(mnameTok.Lexeme)},
			Name:   mnameTok.Lexeme,
			Params: params,
		}
		if p.at(lexer.KindPunct, ":") {
			p.advance()
			p.skipNewlines() // ReturnAnnot: type may wrap to next line (grammar §SyntaxNewlineSuppression)
			rt, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			sig.ReturnType = rt
			sig.Span.EndLine, sig.Span.EndCol = rt.NodeSpan().EndLine, rt.NodeSpan().EndCol
		}
		methods = append(methods, sig)
		p.skipNewlines()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	return &ast.InterfaceDecl{
		Span: ast.Span{StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1},
		Name:       nameTok.Lexeme,
		TypeParams: typeParams,
		Extends:    extends,
		Methods:    methods,
	}, nil
}

func (p *parser) parseClassField() (*ast.ClassField, *Diag) {
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
	return &ast.ClassField{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: ty.NodeSpan().EndLine, EndCol: ty.NodeSpan().EndCol,
		},
		Name:       nameTok.Lexeme,
		DeclType:   ty,
		Mutability: mut,
	}, nil
}

func (p *parser) parseMethod() (*ast.Method, *Diag) {
	startTok := p.peek()
	isStatic := false
	if p.at(lexer.KindKeyword, "static") {
		p.advance()
		isStatic = true
	}
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
	if _, err := p.expect(lexer.KindPunct, ")"); err != nil {
		return nil, err
	}
	var retType ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance()
		p.skipNewlines() // ReturnAnnot: type may wrap to next line (grammar §SyntaxNewlineSuppression)
		retType, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Method{
		Span: ast.Span{
			StartLine: startTok.Line, StartCol: startTok.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Name:       nameTok.Lexeme,
		IsStatic:   isStatic,
		Params:     params,
		ReturnType: retType,
		Body:       body,
	}, nil
}

func (p *parser) parseFuncDecl() (*ast.FuncDecl, *Diag) {
	kw := p.advance() // consume 'func'
	name, err := p.expect(lexer.KindIdent, "")
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
	if _, err := p.expect(lexer.KindPunct, ")"); err != nil {
		return nil, err
	}
	var retType ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance()      // consume ':'
		p.skipNewlines() // ReturnAnnot: type may wrap to next line (grammar §SyntaxNewlineSuppression)
		retType, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncDecl{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Name:       name.Lexeme,
		TypeParams: typeParams,
		Params:     params,
		ReturnType: retType,
		Body:       body,
	}, nil
}

// parseTypeParamList reads an optional `<T, U, ...>` list at
// the declaration head. v1 admits only bare names (default
// constraint `any`); user-written constraints land in PR-G3.
func (p *parser) parseTypeParamList() ([]ast.TypeParam, *Diag) {
	if !p.at(lexer.KindOp, "<") {
		return nil, nil
	}
	p.advance() // consume '<'
	var out []ast.TypeParam
	for {
		p.skipNewlines()
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112",
				fmt.Sprintf("expected type-parameter name, got %s %q", t.Kind, t.Lexeme),
				t.Line, t.Col)
		}
		tp := p.advance()
		param := ast.TypeParam{Name: tp.Lexeme}
		// Optional constraint bound `<T: Ordered>`. The bound name is a
		// built-in generic constraint, validated in sema (E0119); the parser
		// only captures it. Bound-less parameters default to `any`.
		if p.at(lexer.KindPunct, ":") {
			p.advance() // consume ':'
			if !p.at(lexer.KindIdent) {
				t := p.peek()
				return nil, p.diag("E0112",
					fmt.Sprintf("expected a constraint name after `:`, got %s %q", t.Kind, t.Lexeme),
					t.Line, t.Col)
			}
			param.Bound = p.advance().Lexeme
		}
		out = append(out, param)
		if p.at(lexer.KindPunct, ",") {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.KindOp, ">"); err != nil {
		return nil, err
	}
	return out, nil
}

// parseParamList reads zero or more comma-separated `name: T`
// parameters up to (but not consuming) the closing ')'.
func (p *parser) parseParamList() ([]*ast.Param, *Diag) {
	var out []*ast.Param
	p.skipNewlines()
	if p.at(lexer.KindPunct, ")") {
		return nil, nil
	}
	for {
		p.skipNewlines()
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112", "expected parameter name", t.Line, t.Col)
		}
		nameTok := p.advance()
		if _, err := p.expect(lexer.KindPunct, ":"); err != nil {
			return nil, err
		}
		// A leading `...` marks a variadic parameter — its DeclType is
		// the element type T (the parameter is in scope as `[]T`).
		// grammar.ebnf §Param; ffi.md §Variadic.
		variadic := false
		if p.at(lexer.KindOp, "...") {
			p.advance()
			variadic = true
		}
		ty, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		out = append(out, &ast.Param{
			Span: ast.Span{
				StartLine: nameTok.Line, StartCol: nameTok.Col,
				EndLine: ty.NodeSpan().EndLine, EndCol: ty.NodeSpan().EndCol,
			},
			Name:     nameTok.Lexeme,
			DeclType: ty,
			Variadic: variadic,
		})
		p.skipNewlines()
		if !p.at(lexer.KindPunct, ",") {
			break
		}
		// A variadic parameter must be last (E0115): a `,` after one
		// means a further parameter follows.
		if variadic {
			t := p.peek()
			return nil, p.diag("E0115", "A variadic parameter must be the last parameter", t.Line, t.Col)
		}
		p.advance() // consume ','
		// A trailing comma before `)` is allowed (idiomatic in a multi-line
		// signature) — stop rather than demand another parameter.
		p.skipNewlines()
		if p.at(lexer.KindPunct, ")") {
			break
		}
	}
	return out, nil
}
