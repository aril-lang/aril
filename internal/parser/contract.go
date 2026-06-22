package parser

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// parseContractDecl parses a separable value/state contract block
// `contract <target> { <clause>* }` (RFC-0006, grammar.ebnf §ContractBlock).
// The clause keywords (requires/ensures/invariant/loop) are contextual —
// recognized by lexeme only inside the block. Precondition: the cursor is at
// `contract <Ident> {` (verified by atContractBlock).
func (p *parser) parseContractDecl() (*ast.ContractDecl, *Diag) {
	kw := p.advance()     // 'contract'
	target := p.advance() // target name
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipStmtSeps()
	var clauses []ast.ContractClause
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		cl, err := p.parseContractClause()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, cl)
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	return &ast.ContractDecl{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Target:  target.Lexeme,
		Clauses: clauses,
	}, nil
}

// parseContractClause parses one clause of a contract block: a `requires` /
// `ensures` / `invariant` predicate, or a `loop <label> { … }` section.
func (p *parser) parseContractClause() (ast.ContractClause, *Diag) {
	t := p.peek()
	if t.Kind == lexer.KindIdent {
		switch t.Lexeme {
		case "requires", "ensures", "invariant":
			kw := p.advance()
			pred, err := p.parseExpr()
			if err != nil {
				return ast.ContractClause{}, err
			}
			return ast.ContractClause{
				Span: clauseSpan(kw, pred),
				Kind: kw.Lexeme,
				Pred: pred,
			}, nil
		case "loop":
			return p.parseLoopContract()
		case "entry":
			return p.parseEntrySection()
		}
	}
	return ast.ContractClause{}, p.diag("E0112",
		fmt.Sprintf("expected a contract clause (requires/ensures/invariant/loop/entry), got %s %q",
			t.Kind, t.Lexeme), t.Line, t.Col)
}

// parseEntrySection parses an `entry { (let <name> = <expr>)* }` section —
// pure bindings evaluated at function entry and in scope for `ensures`
// (RFC-0006; the general entry-snapshot mechanism, subsuming `old(e)`). v1
// admits only `let` bindings: a `var` (mutable / ghost variable) has no
// mutation site here and is deferred.
func (p *parser) parseEntrySection() (ast.ContractClause, *Diag) {
	kw := p.advance() // 'entry'
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return ast.ContractClause{}, err
	}
	p.skipStmtSeps()
	var binds []ast.ContractBinding
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if p.at(lexer.KindKeyword, "var") {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112",
				"a contract `entry` binding must be `let` (immutable); `var` is not admitted in v1", t.Line, t.Col)
		}
		if _, err := p.expect(lexer.KindKeyword, "let"); err != nil {
			return ast.ContractClause{}, err
		}
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112", "expected binding name after `let`", t.Line, t.Col)
		}
		name := p.advance()
		if _, err := p.expect(lexer.KindOp, "="); err != nil {
			return ast.ContractClause{}, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return ast.ContractClause{}, err
		}
		binds = append(binds, ast.ContractBinding{
			Span: ast.Span{
				StartLine: name.Line, StartCol: name.Col,
				EndLine: val.NodeSpan().EndLine, EndCol: val.NodeSpan().EndCol,
			},
			Name:  name.Lexeme,
			Value: val,
		})
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return ast.ContractClause{}, err
	}
	return ast.ContractClause{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Kind:     "entry",
		Bindings: binds,
	}, nil
}

// parseLoopContract parses a `loop <label> { (invariant <expr>)* }` section —
// per-iteration invariants attached to the labelled loop of the same name in
// the target's body (RFC-0006 loop contracts). A loop section admits only
// `invariant` clauses.
func (p *parser) parseLoopContract() (ast.ContractClause, *Diag) {
	kw := p.advance() // 'loop'
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return ast.ContractClause{}, p.diag("E0112", "expected loop label after `loop`", t.Line, t.Col)
	}
	label := p.advance()
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return ast.ContractClause{}, err
	}
	p.skipStmtSeps()
	var invs []ast.ContractClause
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if !p.at(lexer.KindIdent, "invariant") {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112",
				"a loop contract section admits only `invariant` clauses", t.Line, t.Col)
		}
		ikw := p.advance()
		pred, err := p.parseExpr()
		if err != nil {
			return ast.ContractClause{}, err
		}
		invs = append(invs, ast.ContractClause{
			Span: clauseSpan(ikw, pred),
			Kind: "invariant",
			Pred: pred,
		})
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return ast.ContractClause{}, err
	}
	return ast.ContractClause{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Kind:  "loop",
		Label: label.Lexeme,
		Loop:  invs,
	}, nil
}

// clauseSpan spans from a clause keyword token to the end of its predicate.
func clauseSpan(kw lexer.Token, pred ast.Expr) ast.Span {
	end := pred.NodeSpan()
	return ast.Span{
		StartLine: kw.Line, StartCol: kw.Col,
		EndLine: end.EndLine, EndCol: end.EndCol,
	}
}
