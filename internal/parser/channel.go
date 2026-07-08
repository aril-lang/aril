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
			"never more than … in flight/drains-before-scope-exit/drains-before-return), got %s",
			describeToken(start)), start.Line, start.Col)
}

// ---- RFC-0007 protocol clauses (inside a `contract` body) ----
//
// A `contract` body may host cross-channel *protocol* clauses alongside the
// RFC-0006 value/state clauses (one contract framework). These parse into
// ContractClause with the protocol-clause Kinds; the event operands are
// ordinary Aril Exprs of the form `subject.op(payload)` (a Call/Field chain),
// validated by the contract pass, not the grammar. Dispatched from
// parseContractClause.

// parseSubjectDecl parses a subject declaration `channel <name> [role <role>]`
// inside a contract body (no braces — distinct from the top-level
// `channel <name> { … }` block). The role label (cancel/timeout/signal) is
// validated by the contract pass.
func (p *parser) parseSubjectDecl() (ast.ContractClause, *Diag) {
	kw := p.advance() // 'channel'
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return ast.ContractClause{}, p.diag("E0112", "expected a subject name after `channel`", t.Line, t.Col)
	}
	name := p.advance()
	cl := ast.ContractClause{Kind: "channel-subject", Subject: name.Lexeme}
	last := name
	if p.at(lexer.KindIdent, "role") {
		p.advance() // 'role'
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112",
				"expected a role (cancel/timeout/signal) after `role`", t.Line, t.Col)
		}
		role := p.advance()
		cl.Role = role.Lexeme
		last = role
	}
	cl.Span = spanTokens(kw, last)
	return cl, nil
}

// parseParticipantDecl parses `participant <name> [: <Type>]` — a fan-out
// member or member set (e.g. `participant subscribers: Set<Subscriber>`).
func (p *parser) parseParticipantDecl() (ast.ContractClause, *Diag) {
	kw := p.advance() // 'participant'
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return ast.ContractClause{}, p.diag("E0112", "expected a participant name after `participant`", t.Line, t.Col)
	}
	name := p.advance()
	cl := ast.ContractClause{Kind: "participant", Subject: name.Lexeme}
	if p.at(lexer.KindPunct, ":") {
		p.advance() // ':'
		ty, err := p.parseTypeExpr()
		if err != nil {
			return ast.ContractClause{}, err
		}
		cl.PartType = ty
		end := ty.NodeSpan()
		cl.Span = ast.Span{StartLine: kw.Line, StartCol: kw.Col, EndLine: end.EndLine, EndCol: end.EndCol}
		return cl, nil
	}
	cl.Span = spanTokens(kw, name)
	return cl, nil
}

// parseTwoEventClause parses a two-event protocol clause: the leading keyword,
// the first event Expr, the infix word, the second event Expr. Used by the
// forbid-before / eventually-after / every-eventually forms.
func (p *parser) parseTwoEventClause(kind, infix string) (ast.ContractClause, *Diag) {
	kw := p.advance() // leading keyword (forbid/eventually/every)
	a, err := p.parseExpr()
	if err != nil {
		return ast.ContractClause{}, err
	}
	if !p.at(lexer.KindIdent, infix) {
		t := p.peek()
		return ast.ContractClause{}, p.diag("E0112",
			fmt.Sprintf("expected `%s` between the two events of a `%s` clause", infix, kw.Lexeme), t.Line, t.Col)
	}
	p.advance() // infix word
	b, err := p.parseExpr()
	if err != nil {
		return ast.ContractClause{}, err
	}
	bs := b.NodeSpan()
	return ast.ContractClause{
		Span:   ast.Span{StartLine: kw.Line, StartCol: kw.Col, EndLine: bs.EndLine, EndCol: bs.EndCol},
		Kind:   kind,
		EventA: a,
		EventB: b,
	}, nil
}

// atDeliveredToAll reports whether the cursor is at a fan-out clause
// `<subject> delivered-to-all …` — a subject ident followed by the
// `delivered-to-all` phrase-keyword (which lexes as `delivered - to - all`).
func (p *parser) atDeliveredToAll() bool {
	return p.at(lexer.KindIdent) &&
		p.peekAhead(1).Lexeme == "delivered" && p.peekAhead(2).Lexeme == "-" &&
		p.peekAhead(3).Lexeme == "to" && p.peekAhead(4).Lexeme == "-" &&
		p.peekAhead(5).Lexeme == "all"
}

// parseDeliveredToAll parses a coverage clause
// `<subject> delivered-to-all ({ m1, m2, … } | <receiverSet>)`. Precondition:
// atDeliveredToAll() is true.
func (p *parser) parseDeliveredToAll() (ast.ContractClause, *Diag) {
	subj := p.advance() // subject ident
	p.advanceN(5)       // delivered - to - all
	cl := ast.ContractClause{Kind: "delivered-to-all", Subject: subj.Lexeme}
	if p.at(lexer.KindPunct, "{") {
		p.advance() // '{'
		p.skipNewlines()
		var names []string
		for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
			if !p.at(lexer.KindIdent) {
				t := p.peek()
				return ast.ContractClause{}, p.diag("E0112", "expected a member name in the fan-out set", t.Line, t.Col)
			}
			names = append(names, p.advance().Lexeme)
			p.skipNewlines()
			if p.at(lexer.KindPunct, ",") {
				p.advance()
				p.skipNewlines()
			}
		}
		closeTok, err := p.expect(lexer.KindPunct, "}")
		if err != nil {
			return ast.ContractClause{}, err
		}
		cl.Names = names
		cl.Span = spanTokens(subj, closeTok)
		return cl, nil
	}
	if !p.at(lexer.KindIdent) {
		t := p.peek()
		return ast.ContractClause{}, p.diag("E0112",
			"expected a receiver-set name or a `{ … }` member set after `delivered-to-all`", t.Line, t.Col)
	}
	rs := p.advance()
	cl.RecvSet = rs.Lexeme
	cl.Span = spanTokens(subj, rs)
	return cl, nil
}

// parseFairness parses `fairness { (no-starvation <subject>)* }` — the
// no-starvation subjects collected into Names.
func (p *parser) parseFairness() (ast.ContractClause, *Diag) {
	kw := p.advance() // 'fairness'
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return ast.ContractClause{}, err
	}
	p.skipStmtSeps()
	var names []string
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		if !p.atWords("no", "-", "starvation") {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112",
				"a `fairness` block admits only `no-starvation <subject>` clauses", t.Line, t.Col)
		}
		p.advanceN(3)
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return ast.ContractClause{}, p.diag("E0112", "expected a subject name after `no-starvation`", t.Line, t.Col)
		}
		names = append(names, p.advance().Lexeme)
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return ast.ContractClause{}, err
	}
	return ast.ContractClause{Span: spanTokens(kw, closeTok), Kind: "fairness", Names: names}, nil
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
