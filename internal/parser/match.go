package parser

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// Form: `match Subject { Pat => Body (,|nl) ... }`.
func (p *parser) parseMatchExpr() (*ast.MatchExpr, *Diag) {
	kw := p.advance() // consume 'match'
	subject, err := p.withNoBrace(p.parseExpr)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var arms []*ast.MatchArm
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		armStart := p.peek()
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.KindOp, "=>"); err != nil {
			return nil, err
		}
		p.skipNewlines() // MatchArm: body may wrap to next line (grammar §SyntaxNewlineSuppression)
		body, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		// A braceless assignment (`Some(h) => h.prev = x`) is a
		// side-effecting arm body. The grammar's MatchArm body is an
		// Expr, so wrap the assignment in an implicit unit Block —
		// the existing block-arm lowering then applies (grammar
		// §SyntaxNewlineSuppression note on MatchArm; AssignStmt).
		if assign, aerr := p.parseAssignmentTail(body); aerr != nil {
			return nil, aerr
		} else if assign != nil {
			body = &ast.Block{Span: assign.Span, Stmts: []ast.Stmt{assign}}
		}
		arms = append(arms, &ast.MatchArm{
			Span: ast.Span{
				StartLine: armStart.Line, StartCol: armStart.Col,
				EndLine: body.NodeSpan().EndLine, EndCol: body.NodeSpan().EndCol,
			},
			Pattern: pat,
			Body:    body,
		})
		p.skipNewlines()
		// Per grammar.ebnf:512 — comma separates arms; an
		// optional trailing comma is admitted after the last
		// arm. So: if next is `}` we're done; otherwise a
		// comma must follow.
		if p.at(lexer.KindPunct, "}") {
			break
		}
		if !p.at(lexer.KindPunct, ",") {
			t := p.peek()
			return nil, p.diag("E0112",
				"expected `,` between match arms (or `}` to close)",
				t.Line, t.Col)
		}
		p.advance() // consume ','
		p.skipNewlines()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	if len(arms) == 0 {
		return nil, p.diag("E0112", "match needs at least one arm", kw.Line, kw.Col)
	}
	return &ast.MatchExpr{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Subject: subject,
		Arms:    arms,
	}, nil
}

// parseCatchTail parses the postfix `catch e { block }` after its subject and
// desugars it to a two-arm match (desugaring.md §Catch):
// `match subject { Ok(__aril_catch_v) => __aril_catch_v, Err(e) => { block } }`,
// tagged FromCatch so sema enforces the Err block diverges (E0406). The block
// shares the enclosing function's frame — a `return` in it returns from that
// function — because a match arm, unlike a closure, is not a new return frame.
func (p *parser) parseCatchTail(subject ast.Expr) (ast.Expr, *Diag) {
	kw := p.advance() // consume 'catch'
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return nil, p.diag("E0112",
			fmt.Sprintf("expected an error binder identifier after `catch`, got %s %q", t.Kind, t.Lexeme),
			t.Line, t.Col)
	}
	binder := p.advance()
	block, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	kwSpan := ast.Span{StartLine: kw.Line, StartCol: kw.Col, EndLine: kw.Line, EndCol: kw.Col + utf8.RuneCountInString(kw.Lexeme)}
	binderSpan := ast.Span{
		StartLine: binder.Line, StartCol: binder.Col,
		EndLine: binder.Line, EndCol: binder.Col + utf8.RuneCountInString(binder.Lexeme),
	}
	const okBinder = "__aril_catch_v" // fresh Ok binder; the arm body just returns it (unwrap)
	okArm := &ast.MatchArm{
		Span:    kwSpan,
		Pattern: &ast.VariantPat{Span: kwSpan, QName: []string{"Ok"}, Sub: []ast.Pattern{&ast.IdentPat{Span: kwSpan, Name: okBinder}}},
		Body:    &ast.Ident{Span: kwSpan, Name: okBinder},
	}
	errArm := &ast.MatchArm{
		Span:    block.Span,
		Pattern: &ast.VariantPat{Span: binderSpan, QName: []string{"Err"}, Sub: []ast.Pattern{&ast.IdentPat{Span: binderSpan, Name: binder.Lexeme}}},
		Body:    block,
	}
	return &ast.MatchExpr{
		Span: ast.Span{
			StartLine: subject.NodeSpan().StartLine, StartCol: subject.NodeSpan().StartCol,
			EndLine: block.Span.EndLine, EndCol: block.Span.EndCol,
		},
		Subject:   subject,
		Arms:      []*ast.MatchArm{okArm, errArm},
		FromCatch: true,
		CatchKw:   kwSpan,
	}, nil
}

// parsePattern parses a full pattern: a single pattern, or — when
// `|`-separated atoms follow — an AltPat (grammar.ebnf §Pattern).
// Alternation is greedy here; the `|` is unambiguous in pattern
// position (match arms separate with `,`/newline/`=>`, `for` with
// `in`), so consuming it never conflicts with a single-pattern site.
func (p *parser) parsePattern() (ast.Pattern, *Diag) {
	first, err := p.parseSinglePat()
	if err != nil {
		return nil, err
	}
	if !p.at(lexer.KindOp, "|") {
		return first, nil
	}
	atoms := []ast.Pattern{first}
	end := first.NodeSpan()
	for p.at(lexer.KindOp, "|") {
		p.advance() // consume '|'
		p.skipNewlines()
		atom, err := p.parseSinglePat()
		if err != nil {
			return nil, err
		}
		atoms = append(atoms, atom)
		end = atom.NodeSpan()
	}
	return &ast.AltPat{
		Span: ast.Span{
			StartLine: first.NodeSpan().StartLine, StartCol: first.NodeSpan().StartCol,
			EndLine: end.EndLine, EndCol: end.EndCol,
		},
		Atoms: atoms,
	}, nil
}

// parseSinglePat admits IdentPat, WildcardPat (`_`), IntLitPat,
// FloatLitPat, StringLitPat, RuneLitPat, BoolLitPat, TuplePat,
// VariantPat (with optional payload subpatterns), and RecordPat
// (`Type{ field: pat, … }`). AltPat is assembled by parsePattern
// from these.
func (p *parser) parseSinglePat() (ast.Pattern, *Diag) {
	t := p.peek()
	// TuplePat: `(p1, p2, ...)` — arity ≥ 2; or UnitPat: `()`.
	if t.Kind == lexer.KindPunct && t.Lexeme == "(" {
		open := p.advance() // consume '('
		p.skipNewlines()
		// `()` — the unit pattern (grammar.ebnf §Pattern). Distinct from
		// a tuple pattern; matches the unit value, binds nothing.
		if p.at(lexer.KindPunct, ")") {
			closeTok := p.advance() // consume ')'
			return &ast.UnitPat{
				Span: ast.Span{
					StartLine: open.Line, StartCol: open.Col,
					EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
				},
			}, nil
		}
		var sub []ast.Pattern
		for !p.at(lexer.KindPunct, ")") && !p.at(lexer.KindEOF) {
			sp, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			sub = append(sub, sp)
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
		if len(sub) < 2 {
			return nil, p.diag("E0112", "tuple pattern needs at least two components", open.Line, open.Col)
		}
		return &ast.TuplePat{
			Span: ast.Span{
				StartLine: open.Line, StartCol: open.Col,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
			},
			Sub: sub,
		}, nil
	}
	switch t.Kind {
	case lexer.KindIdent:
		// Could be IdentPat or VariantPat. PR-F2 uses
		// capitalisation as a parser-time proxy for the
		// resolution-time check documented in
		// name-resolution.md §Variant constructors (which
		// notes that the canonical algorithm is resolution-
		// time, not parser-commit). A VariantPat may also be
		// qualified: `Type.V`. The resolver may later reclass
		// IdentPat as VariantPat for in-scope variants —
		// parser only commits the shape based on syntax.
		nameTok := p.advance()
		qname := []string{nameTok.Lexeme}
		endLine, endCol := nameTok.Line, nameTok.Col+utf8.RuneCountInString(nameTok.Lexeme)
		for p.at(lexer.KindPunct, ".") {
			p.advance() // consume '.'
			if !p.at(lexer.KindIdent) {
				t := p.peek()
				return nil, p.diag("E0112", "expected identifier after `.` in pattern", t.Line, t.Col)
			}
			next := p.advance()
			qname = append(qname, next.Lexeme)
			endLine, endCol = next.Line, next.Col+utf8.RuneCountInString(next.Lexeme)
		}
		// RecordPat: `Type{ field: pat, … }` (grammar.ebnf §RecordPat).
		// A capitalised name followed by `{` is a record-destructure
		// pattern, not a nullary variant — parse it before the
		// VariantPat branch so the `{` is consumed here.
		if isCapitalised(nameTok.Lexeme) && p.at(lexer.KindPunct, "{") {
			return p.parseRecordPat(qname, nameTok)
		}
		if isCapitalised(nameTok.Lexeme) || len(qname) > 1 || p.at(lexer.KindPunct, "(") {
			vp := &ast.VariantPat{
				Span: ast.Span{
					StartLine: nameTok.Line, StartCol: nameTok.Col,
					EndLine: endLine, EndCol: endCol,
				},
				QName: qname,
			}
			if p.at(lexer.KindPunct, "(") {
				p.advance() // consume '('
				p.skipNewlines()
				for !p.at(lexer.KindPunct, ")") {
					sp, err := p.parsePattern()
					if err != nil {
						return nil, err
					}
					vp.Sub = append(vp.Sub, sp)
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
				vp.Span.EndLine = closeTok.Line
				vp.Span.EndCol = closeTok.Col + 1
			}
			return vp, nil
		}
		// Lower-case → IdentPat. `_` is handled as WildcardPat
		// via the Ident path; intercept here.
		if nameTok.Lexeme == "_" {
			return &ast.WildcardPat{
				Span: ast.Span{
					StartLine: nameTok.Line, StartCol: nameTok.Col,
					EndLine: nameTok.Line, EndCol: nameTok.Col + 1,
				},
			}, nil
		}
		return &ast.IdentPat{
			Span: ast.Span{
				StartLine: nameTok.Line, StartCol: nameTok.Col,
				EndLine: nameTok.Line, EndCol: nameTok.Col + utf8.RuneCountInString(nameTok.Lexeme),
			},
			Name: nameTok.Lexeme,
		}, nil
	case lexer.KindIntLit:
		p.advance()
		v, err := parseIntLit(t.Lexeme)
		if err != nil {
			return nil, p.diag("E0109", "Malformed numeric literal", t.Line, t.Col)
		}
		return &ast.IntLitPat{
			Span:    spanFromToken(t),
			RawText: t.Lexeme,
			Value:   v,
		}, nil
	case lexer.KindFloatLit:
		p.advance()
		fv, err := strconv.ParseFloat(t.Lexeme, 64)
		if err != nil {
			return nil, p.diag("E0109", "Malformed numeric literal", t.Line, t.Col)
		}
		// Grammar-legal but sema rejects with E0305.
		return &ast.FloatLitPat{
			Span:    spanFromToken(t),
			RawText: t.Lexeme,
			Value:   fv,
		}, nil
	case lexer.KindStringLit:
		p.advance()
		val, err := decodeStringLit(t.Lexeme)
		if err != nil {
			return nil, p.diag("E0110", "Malformed escape sequence", t.Line, t.Col)
		}
		return &ast.StringLitPat{
			Span:  spanFromToken(t),
			Value: val,
		}, nil
	case lexer.KindRuneLit:
		p.advance()
		rv, err := decodeRuneLit(t.Lexeme)
		if err != nil {
			return nil, p.diag("E0110", "Malformed escape sequence", t.Line, t.Col)
		}
		return &ast.RuneLitPat{
			Span:    spanFromToken(t),
			RawText: t.Lexeme,
			Value:   rv,
		}, nil
	case lexer.KindKeyword:
		switch t.Lexeme {
		case "true":
			p.advance()
			return &ast.BoolLitPat{Span: spanFromToken(t), Value: true}, nil
		case "false":
			p.advance()
			return &ast.BoolLitPat{Span: spanFromToken(t), Value: false}, nil
		}
	}
	return nil, p.diag("E0112",
		fmt.Sprintf("expected pattern, got %s %q", t.Kind, t.Lexeme),
		t.Line, t.Col)
}

// parseRecordPat parses `Type{ field: pat (, field: pat)* ,? }` with the
// cursor at the opening `{` (grammar.ebnf §RecordPat). qname is the
// already-parsed record type name; nameTok carries the start position.
// Record punning is intentionally omitted in v1 — every field is the
// explicit `name: Pattern` form.
func (p *parser) parseRecordPat(qname []string, nameTok lexer.Token) (ast.Pattern, *Diag) {
	p.advance() // consume '{'
	p.skipNewlines()
	var fields []*ast.RecordPatField
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112",
				fmt.Sprintf("expected record field name in pattern, got %s %q", t.Kind, t.Lexeme),
				t.Line, t.Col)
		}
		fnameTok := p.advance()
		if _, err := p.expect(lexer.KindPunct, ":"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		sub, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		fields = append(fields, &ast.RecordPatField{
			Span: ast.Span{
				StartLine: fnameTok.Line, StartCol: fnameTok.Col,
				EndLine: sub.NodeSpan().EndLine, EndCol: sub.NodeSpan().EndCol,
			},
			Name:    fnameTok.Lexeme,
			Pattern: sub,
		})
		p.skipNewlines()
		if !p.at(lexer.KindPunct, ",") {
			break
		}
		p.advance() // consume ','
		p.skipNewlines()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, p.diag("E0112", "record pattern needs at least one field", nameTok.Line, nameTok.Col)
	}
	return &ast.RecordPat{
		Span: ast.Span{
			StartLine: nameTok.Line, StartCol: nameTok.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		QName:  qname,
		Fields: fields,
	}, nil
}

func isCapitalised(s string) bool {
	if s == "" {
		return false
	}
	r := s[0]
	return r >= 'A' && r <= 'Z'
}
