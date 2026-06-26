package parser

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// parseChannelDecl parses a separable channel-contract block
// `channel <subject> { <channel-clause>* }` (RFC-0007, grammar.ebnf
// §ChannelBlock). Clause keywords are contextual — recognized by lexeme
// only inside the block. The hyphenated phrase-keywords (`closed-by`,
// `drains-before-scope-exit`) lex as `ident - ident` token runs, so the
// clauses are matched as lexeme sequences (atWords), not single tokens.
// Precondition: the cursor is at `channel <Ident> {` (verified by
// atContractBlock).
func (p *parser) parseChannelDecl() (*ast.ChannelDecl, *Diag) {
	kw := p.advance()      // 'channel'
	subject := p.advance() // subject name
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipStmtSeps()
	var clauses []ast.ChannelClause
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		cl, err := p.parseChannelClause()
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
	return &ast.ChannelDecl{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Subject: subject.Lexeme,
		Clauses: clauses,
	}, nil
}

// parseChannelClause parses one local channel clause (RFC-0007 §Design):
// `closed-by <owner>`, `forbid send after close`, `forbid recv after close`,
// `never more than <expr> in flight`, `drains-before-scope-exit`, or
// `drains-before-return`.
func (p *parser) parseChannelClause() (ast.ChannelClause, *Diag) {
	start := p.peek()
	switch {
	case p.atWords("closed", "-", "by"):
		p.advanceN(3)
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return ast.ChannelClause{}, p.diag("E0112", "expected owner name after `closed-by`", t.Line, t.Col)
		}
		owner := p.advance()
		return ast.ChannelClause{
			Span:  spanTokens(start, owner),
			Kind:  "closed-by",
			Owner: owner.Lexeme,
		}, nil

	case p.atWords("forbid", "send", "after", "close"):
		last := p.advanceN(4)
		return ast.ChannelClause{Span: spanTokens(start, last), Kind: "forbid-send-after-close"}, nil

	case p.atWords("forbid", "recv", "after", "close"):
		last := p.advanceN(4)
		return ast.ChannelClause{Span: spanTokens(start, last), Kind: "forbid-recv-after-close"}, nil

	case p.atWords("never", "more", "than"):
		p.advanceN(3)
		bound, err := p.parseExpr()
		if err != nil {
			return ast.ChannelClause{}, err
		}
		if !p.atWords("in", "flight") {
			t := p.peek()
			return ast.ChannelClause{}, p.diag("E0112",
				"expected `in flight` after the capacity bound in a `never more than …` clause", t.Line, t.Col)
		}
		last := p.advanceN(2)
		return ast.ChannelClause{
			Span:  spanTokens(start, last),
			Kind:  "capacity",
			Bound: bound,
		}, nil

	case p.atWords("drains", "-", "before", "-", "scope", "-", "exit"):
		last := p.advanceN(7)
		return ast.ChannelClause{Span: spanTokens(start, last), Kind: "drains-before-scope-exit"}, nil

	case p.atWords("drains", "-", "before", "-", "return"):
		last := p.advanceN(5)
		return ast.ChannelClause{Span: spanTokens(start, last), Kind: "drains-before-return"}, nil
	}
	return ast.ChannelClause{}, p.diag("E0112",
		fmt.Sprintf("expected a channel clause (closed-by/forbid send|recv after close/"+
			"never more than … in flight/drains-before-scope-exit/drains-before-return), got %s %q",
			start.Kind, start.Lexeme), start.Line, start.Col)
}

// atWords reports whether the next len(words) tokens have exactly these
// lexemes in order. It matches by lexeme only (ignoring token kind), so the
// contextual phrase-keywords whose hyphens lex as `-` operators and whose
// parts may lex as keywords (`in`, `scope`, `return`) are matched uniformly.
func (p *parser) atWords(words ...string) bool {
	for i, w := range words {
		if p.peekAhead(i).Lexeme != w {
			return false
		}
	}
	return true
}

// advanceN consumes n tokens and returns the last one consumed (n >= 1).
func (p *parser) advanceN(n int) lexer.Token {
	var last lexer.Token
	for i := 0; i < n; i++ {
		last = p.advance()
	}
	return last
}

// spanTokens spans from the start of token a to the (half-open) end of
// token b — its column plus its lexeme length.
func spanTokens(a, b lexer.Token) ast.Span {
	return ast.Span{
		StartLine: a.Line, StartCol: a.Col,
		EndLine: b.Line, EndCol: b.Col + len(b.Lexeme),
	}
}
