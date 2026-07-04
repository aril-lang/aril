package parser

import (
	"fmt"
	"unicode/utf8"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// ---- block & statements ----

func (p *parser) parseBlock() (*ast.Block, *Diag) {
	open, err := p.expect(lexer.KindPunct, "{")
	if err != nil {
		return nil, err
	}
	blk := &ast.Block{}
	p.skipStmtSeps()
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		blk.Stmts = append(blk.Stmts, s)
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	blk.Span = ast.Span{
		StartLine: open.Line, StartCol: open.Col,
		EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
	}
	return blk, nil
}

// parseValueBlock parses a `{ ... }` block used in expression
// position (block-as-expression, `if`/`match`-arm bodies). It is
// `parseBlock` plus trailing-expression promotion: per grammar.ebnf
// `Block = "{" ( Stmt Stmtterm )* TrailingExpr? "}"`, a final bare
// expression (one not consumed as a statement-only form) becomes the
// block's value. Diverging trailing forms (`return`) stay statements
// — they have no value to yield.
func (p *parser) parseValueBlock() (*ast.Block, *Diag) {
	blk, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	if n := len(blk.Stmts); n > 0 {
		switch last := blk.Stmts[n-1].(type) {
		case *ast.ExprStmt:
			if !isStmtOnlyExpr(last.Expr) {
				blk.Trailing = last.Expr
				blk.Stmts = blk.Stmts[:n-1]
			}
		case *ast.IfStmt:
			// `if` is an expression (D11); when it is the final form
			// of a value block it is the block's value. Its branch
			// blocks are already value blocks (parseIfStmt parses them
			// with parseValueBlock), so only the node kind changes.
			blk.Trailing = ifStmtToExpr(last)
			blk.Stmts = blk.Stmts[:n-1]
		}
	}
	return blk, nil
}

// ifStmtToExpr reinterprets a statement-position `if` as the
// equivalent IfExpr, for when an `if` is the trailing (value) form of
// a value block. The else-if chain is converted recursively; branch
// blocks are shared unchanged (already value blocks).
func ifStmtToExpr(s *ast.IfStmt) *ast.IfExpr {
	e := &ast.IfExpr{Span: s.Span, Cond: s.Cond, ThenBlock: s.ThenBlock}
	switch els := s.Else.(type) {
	case *ast.IfStmt:
		e.Else = ifStmtToExpr(els)
	case *ast.Block:
		e.Else = els
	}
	return e
}

// isStmtOnlyExpr reports whether e is an expression that yields no
// useful value and therefore must not be promoted to a block's
// trailing position: `return` (diverges) and `spawn` (unit-valued,
// registers a goroutine on the enclosing scope — a trailing spawn
// leaves the scope's value at unit, it is not the value itself).
func isStmtOnlyExpr(e ast.Expr) bool {
	switch e.(type) {
	case *ast.ReturnExpr, *ast.SpawnExpr:
		return true
	}
	return false
}

// parseIfExpr parses `if Cond Block ( "else" ( IfExpr | Block ) )?`
// in expression position. The `else` is syntactically optional here
// (a value-position `if` without `else` yields unit). The
// both-arms-required rule for a value `if` (grammar.ebnf IfExpr) is
// not yet enforced in sema — codegen rejects an else-less branch used
// as a value; the proper Barrier-D diagnostic is a follow-up. Branch
// blocks are value-blocks so their trailing expression becomes the
// branch value.
func (p *parser) parseIfExpr() (*ast.IfExpr, *Diag) {
	kw := p.advance() // consume 'if'
	cond, err := p.withNoBrace(p.parseExpr)
	if err != nil {
		return nil, err
	}
	then, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	ie := &ast.IfExpr{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: then.Span.EndLine, EndCol: then.Span.EndCol,
		},
		Cond:      cond,
		ThenBlock: then,
	}
	if p.at(lexer.KindKeyword, "else") {
		p.advance() // consume 'else'
		if p.at(lexer.KindKeyword, "if") {
			elseIf, err := p.parseIfExpr()
			if err != nil {
				return nil, err
			}
			ie.Else = elseIf
			ie.Span.EndLine, ie.Span.EndCol = elseIf.Span.EndLine, elseIf.Span.EndCol
		} else {
			elseBlk, err := p.parseValueBlock()
			if err != nil {
				return nil, err
			}
			ie.Else = elseBlk
			ie.Span.EndLine, ie.Span.EndCol = elseBlk.Span.EndLine, elseBlk.Span.EndCol
		}
	}
	return ie, nil
}

func (p *parser) parseStmt() (ast.Stmt, *Diag) {
	switch {
	case p.at(lexer.KindKeyword, "if"):
		return p.parseIfStmt()
	case p.at(lexer.KindKeyword, "for"):
		return p.parseForStmt()
	case p.at(lexer.KindKeyword, "while"):
		return p.parseWhileStmt()
	case p.at(lexer.KindKeyword, "defer"):
		return p.parseDeferStmt()
	case p.at(lexer.KindKeyword, "select"):
		return p.parseSelectStmt()
	case p.at(lexer.KindKeyword, "let"):
		return p.parseLetOrVar(true)
	case p.at(lexer.KindKeyword, "const"):
		// `const` is a surface alias for `let` — both produce
		// an immutable binding. The keyword is kept distinct in
		// the lexer for readability of declarations the user
		// intends as named constants; downstream nodes don't
		// distinguish them.
		return p.parseLetOrVar(true)
	case p.at(lexer.KindKeyword, "var"):
		return p.parseLetOrVar(false)
	case p.at(lexer.KindKeyword, "return"):
		// `return` is a DivergingExpr (ast.md §Expr); when it
		// appears at statement position we wrap it in an
		// ExprStmt rather than introducing a separate ReturnStmt.
		re, err := p.parseReturnExpr()
		if err != nil {
			return nil, err
		}
		return &ast.ExprStmt{Span: re.Span, Expr: re}, nil
	default:
		// Expression statement OR assignment.
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if assign, err := p.parseAssignmentTail(e); err != nil {
			return nil, err
		} else if assign != nil {
			return assign, nil
		}
		return &ast.ExprStmt{Span: e.NodeSpan(), Expr: e}, nil
	}
}

// parseAssignmentTail consumes a `= rhs` or compound-assign (`+=` …)
// tail following an already-parsed lvalue and builds the AssignStmt.
// Returns (nil, nil) when no assignment operator follows — the caller
// then treats the lvalue as a plain expression. Shared by statement
// position and the braceless-assignment match-arm body.
func (p *parser) parseAssignmentTail(lvalue ast.Expr) (*ast.AssignStmt, *Diag) {
	// `lvalue = value` — distinguishing from `==` is a non-issue
	// because `=` is its own Op token (the lexer emits `==` as a
	// single token; bare `=` only appears in assignment position).
	if p.at(lexer.KindOp, "=") {
		p.advance()
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{
			Span: ast.Span{
				StartLine: lvalue.NodeSpan().StartLine, StartCol: lvalue.NodeSpan().StartCol,
				EndLine: rhs.NodeSpan().EndLine, EndCol: rhs.NodeSpan().EndCol,
			},
			LValue: lvalue,
			Value:  rhs,
		}, nil
	}
	// Compound assignment: `lhs += rhs` desugars to `lhs = lhs + rhs`
	// at the AST level. The LValue is reused on both sides — this is
	// correct as long as the LValue has no side-effecting subexpression
	// (a plain identifier or field/index path); sema will tighten this
	// once it lands.
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		if p.at(lexer.KindOp, op) {
			opTok := p.advance()
			rhs, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			binOp := op[:1]
			return &ast.AssignStmt{
				Span: ast.Span{
					StartLine: lvalue.NodeSpan().StartLine, StartCol: lvalue.NodeSpan().StartCol,
					EndLine: rhs.NodeSpan().EndLine, EndCol: rhs.NodeSpan().EndCol,
				},
				LValue: lvalue,
				Value: &ast.Binary{
					Span: ast.Span{
						StartLine: opTok.Line, StartCol: opTok.Col,
						EndLine: rhs.NodeSpan().EndLine, EndCol: rhs.NodeSpan().EndCol,
					},
					Op:    binOp,
					Left:  lvalue,
					Right: rhs,
				},
			}, nil
		}
	}
	return nil, nil
}

func (p *parser) parseLetOrVar(isLet bool) (ast.Stmt, *Diag) {
	kw := p.advance() // consume 'let' or 'var'
	// `let (a, b) = e` — tuple-destructuring binding (let only; `var`
	// keeps a single Name). The pattern parser yields a TuplePat; sema
	// distributes the RHS tuple's components and rejects refutable /
	// arity-mismatched shapes.
	if isLet && p.at(lexer.KindPunct, "(") {
		return p.parseDestructureLet(kw)
	}
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
	// `var x: T` admitted without initialiser — sema will reject
	// per G1, but the AST schema (ast.md:222) makes Value
	// optional. `let` must always have an initialiser.
	var value ast.Expr
	if isLet || p.at(lexer.KindOp, "=") {
		if _, err := p.expect(lexer.KindOp, "="); err != nil {
			return nil, err
		}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		value = v
	}
	endLine, endCol := nameTok.Line, nameTok.Col+utf8.RuneCountInString(nameTok.Lexeme)
	if declType != nil {
		endLine, endCol = declType.NodeSpan().EndLine, declType.NodeSpan().EndCol
	}
	if value != nil {
		endLine, endCol = value.NodeSpan().EndLine, value.NodeSpan().EndCol
	}
	span := ast.Span{
		StartLine: kw.Line, StartCol: kw.Col,
		EndLine: endLine, EndCol: endCol,
	}
	if isLet {
		// LetStmt.Pattern — for PR-F1 always IdentPat; tuple /
		// variant / record destructuring patterns land later.
		pat := &ast.IdentPat{
			Span: ast.Span{
				StartLine: nameTok.Line, StartCol: nameTok.Col,
				EndLine: nameTok.Line, EndCol: nameTok.Col + utf8.RuneCountInString(nameTok.Lexeme),
			},
			Name: nameTok.Lexeme,
		}
		return &ast.LetStmt{
			Span: span, Pattern: pat, DeclType: declType, Value: value,
		}, nil
	}
	return &ast.VarStmt{
		Span: span, Name: nameTok.Lexeme, DeclType: declType, Value: value,
	}, nil
}

// parseDestructureLet parses `let (a, b, ...) = e`. The `(`-led binding
// is a TuplePat (reusing the pattern parser); only `let` admits it (a
// destructuring `var` has no single Name to carry). Component
// irrefutability and tuple-arity checks are sema's (T-Let-Destructure).
func (p *parser) parseDestructureLet(kw lexer.Token) (ast.Stmt, *Diag) {
	pat, err := p.parseSinglePat()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindOp, "="); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	span := ast.Span{
		StartLine: kw.Line, StartCol: kw.Col,
		EndLine: value.NodeSpan().EndLine, EndCol: value.NodeSpan().EndCol,
	}
	return &ast.LetStmt{Span: span, Pattern: pat, Value: value}, nil
}

// parseReturnExpr parses `return` or `return <expr>` and returns
// it as a ReturnExpr (DivergingExpr per ast.md). Callers at
// statement position wrap it in an ExprStmt.
func (p *parser) parseReturnExpr() (*ast.ReturnExpr, *Diag) {
	kw := p.advance() // consume 'return'
	// Bare `return` ends at end-of-statement (newline, `}`, EOF) or an arm
	// separator `,` — a valueless `return` is a legal braceless match/select
	// arm body (`None => return,`), grammar.ebnf §MatchArm.
	if p.at(lexer.KindNewline) || p.at(lexer.KindPunct, "}") || p.at(lexer.KindPunct, ",") || p.at(lexer.KindEOF) {
		return &ast.ReturnExpr{
			Span: ast.Span{
				StartLine: kw.Line, StartCol: kw.Col,
				EndLine: kw.Line, EndCol: kw.Col + utf8.RuneCountInString("return"),
			},
		}, nil
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.ReturnExpr{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: value.NodeSpan().EndLine, EndCol: value.NodeSpan().EndCol,
		},
		Value: value,
	}, nil
}

func (p *parser) parseIfStmt() (*ast.IfStmt, *Diag) {
	kw := p.advance() // consume 'if'
	cond, err := p.withNoBrace(p.parseExpr)
	if err != nil {
		return nil, err
	}
	then, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	stmt := &ast.IfStmt{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: then.Span.EndLine, EndCol: then.Span.EndCol,
		},
		Cond:      cond,
		ThenBlock: then,
	}
	if p.at(lexer.KindKeyword, "else") {
		p.advance() // consume 'else'
		if p.at(lexer.KindKeyword, "if") {
			elseIf, err := p.parseIfStmt()
			if err != nil {
				return nil, err
			}
			stmt.Else = elseIf
			stmt.Span.EndLine = elseIf.Span.EndLine
			stmt.Span.EndCol = elseIf.Span.EndCol
		} else {
			elseBlk, err := p.parseValueBlock()
			if err != nil {
				return nil, err
			}
			stmt.Else = elseBlk
			stmt.Span.EndLine = elseBlk.Span.EndLine
			stmt.Span.EndCol = elseBlk.Span.EndCol
		}
	}
	return stmt, nil
}

func (p *parser) parseForStmt() (*ast.ForStmt, *Diag) {
	kw := p.advance() // consume 'for'
	// Loop variable is a pattern: a bare name, or a tuple pattern
	// `for (k, v) in m` for key/value (or paired) iteration.
	if !p.at(lexer.KindIdent) && !p.at(lexer.KindPunct, "(") {
		t := p.peek()
		return nil, p.diag("E0112", "expected loop variable name or tuple pattern", t.Line, t.Col)
	}
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindKeyword, "in"); err != nil {
		return nil, err
	}
	savedNB := p.noBrace
	p.noBrace = true
	iter, err := p.parseIterable()
	p.noBrace = savedNB
	if err != nil {
		return nil, err
	}
	label := p.parseOptLoopLabel()
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ForStmt{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Pattern:  pat,
		Iterable: iter,
		Label:    label,
		Body:     body,
	}, nil
}

// parseOptLoopLabel consumes an optional `loop <ident>` loop-contract label
// between a for/while header and its block (grammar.ebnf §LoopLabel). `loop`
// is contextual — a keyword only in this position, an ordinary identifier
// elsewhere — so the claim is guarded: it fires only on `loop` immediately
// followed by an identifier (the label), which no header expression ends
// with (a bare `for x in loop {` leaves `loop` as the iterable, with `{`
// next, not an identifier). Returns "" when absent.
func (p *parser) parseOptLoopLabel() string {
	if p.at(lexer.KindIdent, "loop") && p.peekAhead(1).Kind == lexer.KindIdent {
		p.advance() // 'loop'
		return p.advance().Lexeme
	}
	return ""
}

// parseWhileStmt parses `while Cond Block` (grammar.ebnf WhileStmt).
// The condition is a plain expression; like `if`/`for` headers it
// stops at the body's `{` (a bare `{` is never an expression
// continuation).
func (p *parser) parseWhileStmt() (*ast.WhileStmt, *Diag) {
	kw := p.advance() // consume 'while'
	cond, err := p.withNoBrace(p.parseExpr)
	if err != nil {
		return nil, err
	}
	label := p.parseOptLoopLabel()
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.WhileStmt{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Cond:  cond,
		Label: label,
		Body:  body,
	}, nil
}

// parseSelectStmt parses `select { case … => block, … }` (grammar
// SelectStmt). `case` / `default` are not reserved words — they lex
// as identifiers and are recognised by lexeme only inside the
// `select` body. Cases are separated by `,` or newlines.
func (p *parser) parseSelectStmt() (*ast.SelectStmt, *Diag) {
	kw := p.advance() // consume 'select'
	if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var cases []ast.SelectCase
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		sc, err := p.parseSelectCase()
		if err != nil {
			return nil, err
		}
		cases = append(cases, sc)
		p.skipNewlines()
		if p.at(lexer.KindPunct, ",") {
			p.advance()
			p.skipNewlines()
		}
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	return &ast.SelectStmt{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Cases: cases,
	}, nil
}

// parseSelectCase parses one `select` arm: a receive (`case (x =)?
// <-ch => block`), a send (`case ch.send(v) => block`), or
// `default => block`. The `<-` receive operator is legal only here.
func (p *parser) parseSelectCase() (ast.SelectCase, *Diag) {
	if p.at(lexer.KindIdent, "default") {
		kw := p.advance()
		body, err := p.parseSelectArrowBody()
		if err != nil {
			return nil, err
		}
		return &ast.SelectDefault{
			Span: ast.Span{
				StartLine: kw.Line, StartCol: kw.Col,
				EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
			},
			Body: body,
		}, nil
	}
	if !p.at(lexer.KindIdent, "case") {
		t := p.peek()
		return nil, p.diag("E0112",
			fmt.Sprintf("expected `case` or `default` in select, got %s %q", t.Kind, t.Lexeme),
			t.Line, t.Col)
	}
	caseKw := p.advance() // consume 'case'

	// Receive-and-drop: `case <-ch => block`.
	if p.at(lexer.KindOp, "<-") {
		p.advance()
		ch, err := p.parsePostfix()
		if err != nil {
			return nil, err
		}
		body, err := p.parseSelectArrowBody()
		if err != nil {
			return nil, err
		}
		return &ast.SelectRecv{
			Span: ast.Span{
				StartLine: caseKw.Line, StartCol: caseKw.Col,
				EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
			},
			Channel: ch,
			Body:    body,
		}, nil
	}

	// Either a receive-into-binding `case x = <-ch => …` or a send
	// `case ch.send(v) => …`. Parse the head postfix expression and
	// disambiguate on the following token.
	head, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.KindOp, "=") {
		id, ok := head.(*ast.Ident)
		if !ok {
			t := p.peek()
			return nil, p.diag("E0112", "expected a channel-bind name before `=` in select case", t.Line, t.Col)
		}
		p.advance() // consume '='
		if _, err := p.expect(lexer.KindOp, "<-"); err != nil {
			return nil, err
		}
		ch, err := p.parsePostfix()
		if err != nil {
			return nil, err
		}
		body, err := p.parseSelectArrowBody()
		if err != nil {
			return nil, err
		}
		return &ast.SelectRecv{
			Span: ast.Span{
				StartLine: caseKw.Line, StartCol: caseKw.Col,
				EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
			},
			Bind:    id.Name,
			Channel: ch,
			Body:    body,
		}, nil
	}

	// Send: the head must be a `ch.send(v)` call.
	call, ok := head.(*ast.Call)
	if ok {
		if f, isField := call.Callee.(*ast.Field); isField && f.Name == "send" && len(call.Args) == 1 {
			body, err := p.parseSelectArrowBody()
			if err != nil {
				return nil, err
			}
			return &ast.SelectSend{
				Span: ast.Span{
					StartLine: caseKw.Line, StartCol: caseKw.Col,
					EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
				},
				Channel: f.Receiver,
				Value:   call.Args[0],
				Body:    body,
			}, nil
		}
	}
	t := p.peek()
	return nil, p.diag("E0112",
		"expected a select case: `x = <-ch`, `<-ch`, or `ch.send(v)`",
		t.Line, t.Col)
}

// parseSelectArrowBody consumes `=> Body`, the tail shared by every
// select case. Body is either a `{…}` block or — mirroring the braceless
// MatchArm body — a single statement (`=> return …`, `=> x = y`,
// `=> ch.send(v)`), wrapped in an implicit Block so the existing
// block-body lowering applies unchanged (grammar.ebnf §SelectCaseBody).
func (p *parser) parseSelectArrowBody() (*ast.Block, *Diag) {
	if _, err := p.expect(lexer.KindOp, "=>"); err != nil {
		return nil, err
	}
	p.skipNewlines() // body may wrap to the next line (like MatchArm)
	if p.at(lexer.KindPunct, "{") {
		return p.parseBlock()
	}
	stmt, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	return &ast.Block{Span: stmt.NodeSpan(), Stmts: []ast.Stmt{stmt}}, nil
}

// parseDeferStmt parses `defer <Expr>` (grammar production
// DeferStmt). The argument is a general expression at parse time;
// sema enforces the Call shape (T-Defer / E0406).
func (p *parser) parseDeferStmt() (*ast.DeferStmt, *Diag) {
	kw := p.advance() // consume 'defer'
	call, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.DeferStmt{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: call.NodeSpan().EndLine, EndCol: call.NodeSpan().EndCol,
		},
		Call: call,
	}, nil
}

// parseIterable parses the right-hand side of `for x in <here>`.
// A RangeExpr (a..b / a..=b) is detected by looking for the range
// operator after the first sub-expression. Any other Expr is
// returned directly — Iterable is just `Node`, so both *RangeExpr
// and any concrete Expr satisfy it.
func (p *parser) parseIterable() (ast.Iterable, *Diag) {
	low, err := p.parseAddSubExpr()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.KindOp, "..") || p.at(lexer.KindOp, "..=") {
		op := p.advance()
		high, err := p.parseAddSubExpr()
		if err != nil {
			return nil, err
		}
		return &ast.RangeExpr{
			Span: ast.Span{
				StartLine: low.NodeSpan().StartLine, StartCol: low.NodeSpan().StartCol,
				EndLine: high.NodeSpan().EndLine, EndCol: high.NodeSpan().EndCol,
			},
			Low:       low,
			High:      high,
			Inclusive: op.Lexeme == "..=",
		}, nil
	}
	return low, nil
}
