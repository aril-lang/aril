package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// ---- expressions (precedence climbing) ----
//
// Precedence (high → low), matching grammar.ebnf §Operator table:
//   primary   — literal / ident / paren / call / field
//   unary     — !x, -x
//   mul       — *  /  %
//   add       — +  -
//   cmp       — ==  !=  <  <=  >  >=
//   logical   — &&
//   logical   — ||

func (p *parser) parseExpr() (ast.Expr, *Diag) {
	e, err := p.parseLogicalOr()
	if err != nil {
		return nil, err
	}
	// Postfix `catch e { block }` (grammar.ebnf §CatchExpr) binds looser than
	// every operator — the whole preceding expression is the Result subject.
	// It must cuddle on the same line (a newline ends the subject, D11), like
	// `} else if`. Suppressed under noBrace (match subject / `if` condition),
	// where a trailing `{` is not a block.
	if !p.noBrace && p.at(lexer.KindKeyword, "catch") {
		return p.parseCatchTail(e)
	}
	return e, nil
}

func (p *parser) parseLogicalOr() (ast.Expr, *Diag) {
	return p.parseBinaryL(p.parseLogicalAnd, []string{"||"})
}

func (p *parser) parseLogicalAnd() (ast.Expr, *Diag) {
	return p.parseBinaryL(p.parseEq, []string{"&&"})
}

// parseEq admits a SINGLE optional `==`/`!=` operator over parseCmp,
// matching grammar.ebnf EqExpr = CmpExpr ( ("==" | "!=") CmpExpr )?
// (non-associative).
func (p *parser) parseEq() (ast.Expr, *Diag) {
	return p.parseBinaryOnce(p.parseCmp, []string{"==", "!="})
}

// parseCmp admits a SINGLE optional `<`/`<=`/`>`/`>=` operator over
// parseAddSubExpr, matching grammar.ebnf CmpExpr = AddExpr
// ( ("<"|"<="|">"|">=") AddExpr )? (non-associative).
func (p *parser) parseCmp() (ast.Expr, *Diag) {
	return p.parseBinaryOnce(p.parseAddSubExpr, []string{"<", "<=", ">", ">="})
}

func (p *parser) parseAddSubExpr() (ast.Expr, *Diag) {
	return p.parseBinaryL(p.parseMulDiv, []string{"+", "-"})
}

func (p *parser) parseMulDiv() (ast.Expr, *Diag) {
	return p.parseBinaryL(p.parseUnary, []string{"*", "/", "%"})
}

// parseBinaryL is a left-associative binary-operator helper for a
// single precedence level. ops are the operator lexemes admitted
// at this level.
func (p *parser) parseBinaryL(next func() (ast.Expr, *Diag), ops []string) (ast.Expr, *Diag) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for {
		matched := false
		for _, op := range ops {
			if p.at(lexer.KindOp, op) {
				matched = true
				opTok := p.advance()
				right, err := next()
				if err != nil {
					return nil, err
				}
				left = &ast.Binary{
					Span: ast.Span{
						StartLine: left.NodeSpan().StartLine, StartCol: left.NodeSpan().StartCol,
						EndLine: right.NodeSpan().EndLine, EndCol: right.NodeSpan().EndCol,
					},
					Op:    opTok.Lexeme,
					Left:  left,
					Right: right,
				}
				break
			}
		}
		if !matched {
			return left, nil
		}
	}
}

// parseBinaryOnce admits at most ONE operator from ops over the
// `next` parselet (non-associative). Repeated operators at this
// level (e.g., `a == b == c`) produce E0112.
func (p *parser) parseBinaryOnce(next func() (ast.Expr, *Diag), ops []string) (ast.Expr, *Diag) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if !p.at(lexer.KindOp, op) {
			continue
		}
		opTok := p.advance()
		right, err := next()
		if err != nil {
			return nil, err
		}
		result := &ast.Binary{
			Span: ast.Span{
				StartLine: left.NodeSpan().StartLine, StartCol: left.NodeSpan().StartCol,
				EndLine: right.NodeSpan().EndLine, EndCol: right.NodeSpan().EndCol,
			},
			Op:    opTok.Lexeme,
			Left:  left,
			Right: right,
		}
		// Reject another operator at the same precedence — the
		// production is non-associative.
		for _, op := range ops {
			if p.at(lexer.KindOp, op) {
				t := p.peek()
				return nil, p.diag("E0112",
					fmt.Sprintf("operator %q is non-associative; parenthesise the operands", op),
					t.Line, t.Col)
			}
		}
		return result, nil
	}
	return left, nil
}

func (p *parser) parseUnary() (ast.Expr, *Diag) {
	if p.at(lexer.KindOp, "!") || p.at(lexer.KindOp, "-") {
		op := p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{
			Span: ast.Span{
				StartLine: op.Line, StartCol: op.Col,
				EndLine: operand.NodeSpan().EndLine, EndCol: operand.NodeSpan().EndCol,
			},
			Op:      op.Lexeme,
			Operand: operand,
		}, nil
	}
	return p.parsePostfix()
}

// splitChainedTupleIndex re-splits a FloatLit lexeme `N.M` that the lexer
// produced for a chained tuple index (`r.1.0` lexes `1.0` as one float). It
// accepts only the plain `digits "." digits` shape — an exponent or any
// other float form is not a tuple-index chain, so ok is false.
func splitChainedTupleIndex(lexeme string) (lhs, rhs int, ok bool) {
	a, b, found := strings.Cut(lexeme, ".")
	if !found || a == "" || b == "" {
		return 0, 0, false
	}
	lhs, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	rhs, err = strconv.Atoi(b)
	if err != nil {
		return 0, 0, false
	}
	return lhs, rhs, true
}

func (p *parser) parsePostfix() (ast.Expr, *Diag) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		// Leading-dot continuation: a newline before a `.` does not end
		// the postfix chain (`items⏎  .filter(p)⏎  .map(f)`). Only `.`
		// earns this — any other token after the newline terminates the
		// expression as usual (unbracketed operator continuation is NOT
		// adopted). Decision 2026-06-13; see grammar §PostfixExpr.
		// (Inside brackets the newline is already gone — token filter.)
		if p.at(lexer.KindNewline) && p.peekPastNewlines().Kind == lexer.KindPunct &&
			p.peekPastNewlines().Lexeme == "." {
			p.skipNewlines()
		}
		switch {
		case p.at(lexer.KindPunct, "("):
			call, err := p.parseCallSuffix(e, nil)
			if err != nil {
				return nil, err
			}
			e = call
		case p.at(lexer.KindOp, "<") && p.couldBeGenericCallSite():
			// `<TypeArgs>` postfix per grammar.ebnf
			// §Generic-argument disambiguation. After the closing
			// `>` exactly one of `(`, `{`, or `.` may follow.
			typeArgs, err := p.parseCallTypeArgs()
			if err != nil {
				return nil, err
			}
			switch {
			case p.at(lexer.KindPunct, "("):
				call, err := p.parseCallSuffix(e, typeArgs)
				if err != nil {
					return nil, err
				}
				e = call
			case p.at(lexer.KindPunct, "."):
				// Generic static-method call:
				//   `ClassName<T1, ...>.method(args)`
				// The type-args bind to the *class*, not the
				// method. Build a Field with the class as the
				// receiver, then a Call that wraps the Field and
				// carries the type-args to codegen.
				p.advance() // consume '.'
				if !p.at(lexer.KindIdent) {
					t := p.peek()
					return nil, p.diag("E0112", "expected method name after `.`", t.Line, t.Col)
				}
				name := p.advance()
				field := &ast.Field{
					Span: ast.Span{
						StartLine: e.NodeSpan().StartLine, StartCol: e.NodeSpan().StartCol,
						EndLine: name.Line, EndCol: name.Col + len(name.Lexeme),
					},
					Receiver: e,
					Name:     name.Lexeme,
				}
				call, err := p.parseCallSuffix(field, typeArgs)
				if err != nil {
					return nil, err
				}
				e = call
			case p.at(lexer.KindPunct, "{") && !p.noBrace:
				// Generic brace literal `T<...>{ … }` — container
				// (`Set<int>{1,2}`) or generic record/class
				// (`Broker<T>{ … }`). Shares one path with the bare
				// form; the type-args ride on the NamedType
				// (grammar.ebnf §BraceLit). Suppressed in control-flow
				// headers (noBrace).
				// The QName may be multi-segment for a qualified `pkg.Type`
				// head (`atomic.Pointer<Node>{}`) — via the same
				// qualifiedNameChain the non-generic `sync.Mutex{}` path uses.
				qname, span, ok := qualifiedNameChain(e)
				if !ok {
					t := p.peek()
					return nil, p.diag("E0112", "generic brace literal requires a type name", t.Line, t.Col)
				}
				lit, err := p.parseBraceLitBody(&ast.NamedType{
					Span:  span,
					QName: qname,
					Args:  typeArgs,
				})
				if err != nil {
					return nil, err
				}
				e = lit
			case p.at(lexer.KindPunct, "{"):
				// `{` reached here only with noBrace set: a generic
				// brace literal is ambiguous with the header's block.
				t := p.peek()
				return nil, p.diag("E0112",
					"generic brace literal is ambiguous with the block here — parenthesise it",
					t.Line, t.Col)
			default:
				t := p.peek()
				return nil, p.diag("E0112",
					fmt.Sprintf("expected `(`, `.`, or `{` after generic type arguments, got %s", describeToken(t)),
					t.Line, t.Col)
			}
		case p.at(lexer.KindPunct, "."):
			p.advance()
			// `.N` (integer) is tuple-field access; `.name` is a field.
			if p.at(lexer.KindIntLit) {
				idxTok := p.advance()
				pos, perr := strconv.Atoi(idxTok.Lexeme)
				if perr != nil {
					return nil, p.diag("E0112", "malformed tuple index", idxTok.Line, idxTok.Col)
				}
				e = &ast.TupleField{
					Span: ast.Span{
						StartLine: e.NodeSpan().StartLine, StartCol: e.NodeSpan().StartCol,
						EndLine: idxTok.Line, EndCol: idxTok.Col + len(idxTok.Lexeme),
					},
					Receiver: e,
					Position: pos,
				}
				break
			}
			// Chained tuple index `r.1.0`: the lexer reads `1.0` as one
			// FloatLit, but in an index chain it is two suffixes `.1` then
			// `.0` (grammar §TupleFieldSuffix). Re-split the `N.M` lexeme.
			if p.at(lexer.KindFloatLit) {
				ftok := p.advance()
				lhs, rhs, ok := splitChainedTupleIndex(ftok.Lexeme)
				if !ok {
					return nil, p.diag("E0112", "expected field name after `.`", ftok.Line, ftok.Col)
				}
				inner := &ast.TupleField{
					Span: ast.Span{
						StartLine: e.NodeSpan().StartLine, StartCol: e.NodeSpan().StartCol,
						// Inner `.N` ends at the embedded `.` — use the lexeme
						// prefix so a leading-zero index sizes exactly.
						EndLine: ftok.Line, EndCol: ftok.Col + strings.IndexByte(ftok.Lexeme, '.'),
					},
					Receiver: e,
					Position: lhs,
				}
				e = &ast.TupleField{
					Span: ast.Span{
						StartLine: inner.Span.StartLine, StartCol: inner.Span.StartCol,
						EndLine: ftok.Line, EndCol: ftok.Col + len(ftok.Lexeme),
					},
					Receiver: inner,
					Position: rhs,
				}
				break
			}
			if !p.at(lexer.KindIdent) {
				t := p.peek()
				return nil, p.diag("E0112", "expected field name after `.`", t.Line, t.Col)
			}
			name := p.advance()
			e = &ast.Field{
				Span: ast.Span{
					StartLine: e.NodeSpan().StartLine, StartCol: e.NodeSpan().StartCol,
					EndLine: name.Line, EndCol: name.Col + len(name.Lexeme),
				},
				Receiver: e,
				Name:     name.Lexeme,
			}
		case p.at(lexer.KindPunct, "["):
			next, err := p.parseIndexOrSlice(e)
			if err != nil {
				return nil, err
			}
			e = next
		case p.at(lexer.KindPunct, "{") && !p.noBrace:
			// Brace literal off a bare or qualified name (`sync.Mutex{}`); any
			// other `expr { … }` is not one (grammar.ebnf §BraceLit). Suppressed
			// in control-flow headers (noBrace).
			qname, span, ok := qualifiedNameChain(e)
			if !ok {
				return e, nil
			}
			lit, err := p.parseBraceLitBody(&ast.NamedType{Span: span, QName: qname})
			if err != nil {
				return nil, err
			}
			e = lit
		default:
			return e, nil
		}
	}
}

// qualifiedNameChain flattens a bare ident or a pure dotted name-chain into a
// qualified type name for a `pkg.Type{}` brace literal; ok=false when a link is
// not a name (`f().x`). Span covers the chain (grammar.ebnf §BraceLit).
func qualifiedNameChain(e ast.Expr) ([]string, ast.Span, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return []string{v.Name}, v.Span, true
	case *ast.Field:
		prefix, _, ok := qualifiedNameChain(v.Receiver)
		if !ok {
			return nil, ast.Span{}, false
		}
		return append(prefix, v.Name), v.Span, true
	}
	return nil, ast.Span{}, false
}

// parseBraceLitBody parses `{ … }` after a type name, committing the
// BraceKind on the first entry (RecordEntry `Ident:`, MapEntry
// `MapKey:`, SetEntry bare expr); an empty `{}` stays Unknown for
// sema. Cursor at the opening `{`. The `typeName` carries any generic
// type-args (`Set<int>{…}`), so the generic and bare forms share one
// path (grammar.ebnf §BraceLit). Brace-literal suppression is lifted
// inside the body (entries may themselves contain literals).
func (p *parser) parseBraceLitBody(typeName *ast.NamedType) (*ast.BraceLit, *Diag) {
	p.advance() // consume '{'
	saved := p.noBrace
	p.noBrace = false
	defer func() { p.noBrace = saved }()
	p.skipStmtSeps()
	lit := &ast.BraceLit{
		TypeName: typeName,
		Kind:     ast.BraceUnknown,
	}
	for !p.at(lexer.KindPunct, "}") && !p.at(lexer.KindEOF) {
		entryStart := p.peek()
		// RecordEntry: `Ident :` where the key is a bare identifier.
		if p.at(lexer.KindIdent) && p.peekAhead(1).Kind == lexer.KindPunct && p.peekAhead(1).Lexeme == ":" {
			key := p.advance()
			p.advance() // consume ':'
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if lit.Kind == ast.BraceUnknown {
				lit.Kind = ast.BraceRecord
			} else if lit.Kind != ast.BraceRecord {
				return nil, p.diag("E0112", "mixed brace-literal entry kinds", entryStart.Line, entryStart.Col)
			}
			lit.Entries = append(lit.Entries, &ast.RecordEntry{
				Span:  ast.Span{StartLine: key.Line, StartCol: key.Col, EndLine: val.NodeSpan().EndLine, EndCol: val.NodeSpan().EndCol},
				Name:  key.Lexeme,
				Value: val,
			})
		} else {
			// MapEntry (`key : value`) or SetEntry (bare value).
			keyExpr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if p.at(lexer.KindPunct, ":") {
				p.advance() // consume ':'
				val, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if lit.Kind == ast.BraceUnknown {
					lit.Kind = ast.BraceMap
				} else if lit.Kind != ast.BraceMap {
					return nil, p.diag("E0112", "mixed brace-literal entry kinds", entryStart.Line, entryStart.Col)
				}
				lit.Entries = append(lit.Entries, &ast.MapEntry{
					Span:  ast.Span{StartLine: entryStart.Line, StartCol: entryStart.Col, EndLine: val.NodeSpan().EndLine, EndCol: val.NodeSpan().EndCol},
					Key:   keyExpr,
					Value: val,
				})
			} else {
				if lit.Kind == ast.BraceUnknown {
					lit.Kind = ast.BraceSet
				} else if lit.Kind != ast.BraceSet {
					return nil, p.diag("E0112", "mixed brace-literal entry kinds", entryStart.Line, entryStart.Col)
				}
				lit.Entries = append(lit.Entries, &ast.SetEntry{
					Span:  keyExpr.NodeSpan(),
					Value: keyExpr,
				})
			}
		}
		if p.at(lexer.KindPunct, ",") {
			p.advance()
		}
		p.skipStmtSeps()
	}
	closeTok, err := p.expect(lexer.KindPunct, "}")
	if err != nil {
		return nil, err
	}
	lit.Span = ast.Span{
		StartLine: typeName.Span.StartLine, StartCol: typeName.Span.StartCol,
		EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
	}
	return lit, nil
}

// parseIndexOrSlice parses the postfix `[i]` or `[lo:hi]` /
// `[lo:]` / `[:hi]` form. Cursor at `[`.
func (p *parser) parseIndexOrSlice(recv ast.Expr) (ast.Expr, *Diag) {
	savedNB := p.noBrace
	p.noBrace = false
	defer func() { p.noBrace = savedNB }()
	p.advance() // consume '['
	// `[:hi]` — leading colon.
	if p.at(lexer.KindPunct, ":") {
		p.advance() // consume ':'
		// `[:]` is a copy slice; `[:hi]` has High.
		var high ast.Expr
		if !p.at(lexer.KindPunct, "]") {
			h, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			high = h
		}
		closeTok, err := p.expect(lexer.KindPunct, "]")
		if err != nil {
			return nil, err
		}
		return &ast.Slice{
			Span: ast.Span{
				StartLine: recv.NodeSpan().StartLine, StartCol: recv.NodeSpan().StartCol,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
			},
			Receiver: recv,
			Low:      nil,
			High:     high,
		}, nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.at(lexer.KindPunct, ":") {
		p.advance() // consume ':'
		var high ast.Expr
		if !p.at(lexer.KindPunct, "]") {
			h, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			high = h
		}
		closeTok, err := p.expect(lexer.KindPunct, "]")
		if err != nil {
			return nil, err
		}
		return &ast.Slice{
			Span: ast.Span{
				StartLine: recv.NodeSpan().StartLine, StartCol: recv.NodeSpan().StartCol,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
			},
			Receiver: recv,
			Low:      first,
			High:     high,
		}, nil
	}
	closeTok, err := p.expect(lexer.KindPunct, "]")
	if err != nil {
		return nil, err
	}
	return &ast.Index{
		Span: ast.Span{
			StartLine: recv.NodeSpan().StartLine, StartCol: recv.NodeSpan().StartCol,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Receiver: recv,
		Idx:      first,
	}, nil
}

// couldBeGenericCallSite peeks at the token stream starting at
// the current `<` and returns true iff the `<` opens a
// generic-argument list. Per `lang-spec/grammar.ebnf`
// §Generic-argument disambiguation: commit when the matching
// `>` is followed by one of `(`, `{`, or `.` — function call,
// generic literal, or generic static-method call respectively.
// Otherwise the `<` is the comparison operator.
//
// Implementation is token-only depth-counting (no rewind /
// speculative parse). It is equivalent to the speculative-parse
// shape for the v1 type-argument grammar (Ident, `.`, `[`, `]`,
// `,`, nested `<>`). Adding richer type forms to TypeArgs
// (TupleType, FuncType, ParenType, ...) must extend the
// allowed-token set below or the disambig will silently
// false-negative on those shapes.
func (p *parser) couldBeGenericCallSite() bool {
	pos := p.pos
	if pos >= len(p.toks) || p.toks[pos].Kind != lexer.KindOp || p.toks[pos].Lexeme != "<" {
		return false
	}
	depth := 0
	for i := pos; i < len(p.toks); i++ {
		t := p.toks[i]
		switch {
		case t.Kind == lexer.KindOp && t.Lexeme == "<":
			depth++
		case t.Kind == lexer.KindOp && t.Lexeme == ">":
			depth--
			if depth == 0 {
				// Next non-newline token decides per
				// `grammar.ebnf` §Generic-argument disambiguation:
				// commit on `(`, `{`, or `.`.
				for j := i + 1; j < len(p.toks); j++ {
					n := p.toks[j]
					if n.Kind == lexer.KindNewline {
						continue
					}
					if n.Kind != lexer.KindPunct {
						return false
					}
					return n.Lexeme == "(" || n.Lexeme == "{" || n.Lexeme == "."
				}
				return false
			}
		case t.Kind == lexer.KindNewline,
			t.Kind == lexer.KindIdent,
			t.Kind == lexer.KindKeyword && t.Lexeme == "unit",
			t.Kind == lexer.KindPunct && (t.Lexeme == "," || t.Lexeme == "." || t.Lexeme == "[" || t.Lexeme == "]"):
			// allowed inside a type-arg list (`unit` is a keyword but
			// also the one-inhabitant value-type, legal as a type arg).
			// Newline is insignificant inside `<...>` (grammar
			// §SyntaxNewlineSuppression) — a wrapped `Map<int,⏎ string>`
			// must still be recognised as a generic call site.
		default:
			return false
		}
	}
	return false
}

// couldBeShortClosure peeks from the cursor at `(` to its matching
// `)` and reports whether `=>` immediately follows — i.e. this is a
// short closure `(params) => expr`, not a parenthesised expr / tuple.
func (p *parser) couldBeShortClosure() bool {
	if !p.at(lexer.KindPunct, "(") {
		return false
	}
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		t := p.toks[i]
		if t.Kind == lexer.KindPunct && t.Lexeme == "(" {
			depth++
		} else if t.Kind == lexer.KindPunct && t.Lexeme == ")" {
			depth--
			if depth == 0 {
				for j := i + 1; j < len(p.toks); j++ {
					n := p.toks[j]
					if n.Kind == lexer.KindNewline {
						continue
					}
					return n.Kind == lexer.KindOp && n.Lexeme == "=>"
				}
				return false
			}
		}
	}
	return false
}

// parseShortClosure parses `(p1, p2, ...) => expr` (grammar.ebnf
// ShortParamList). Params may carry an optional `: TypeExpr`. The
// `=> expr` body is wrapped in a Block whose trailing value is expr.
func (p *parser) parseShortClosure() (*ast.ClosureLit, *Diag) {
	open := p.advance() // consume '('
	p.skipNewlines()
	var params []*ast.Param
	for !p.at(lexer.KindPunct, ")") && !p.at(lexer.KindEOF) {
		if !p.at(lexer.KindIdent) {
			t := p.peek()
			return nil, p.diag("E0112", "expected closure parameter name", t.Line, t.Col)
		}
		nameTok := p.advance()
		param := &ast.Param{
			Span: ast.Span{StartLine: nameTok.Line, StartCol: nameTok.Col,
				EndLine: nameTok.Line, EndCol: nameTok.Col + len(nameTok.Lexeme)},
			Name: nameTok.Lexeme,
		}
		if p.at(lexer.KindPunct, ":") {
			p.advance()
			ty, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			param.DeclType = ty
			param.Span.EndLine, param.Span.EndCol = ty.NodeSpan().EndLine, ty.NodeSpan().EndCol
		}
		params = append(params, param)
		p.skipNewlines()
		if !p.at(lexer.KindPunct, ",") {
			break
		}
		p.advance()
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.KindPunct, ")"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KindOp, "=>"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	bodyBlock := &ast.Block{Span: body.NodeSpan(), Trailing: body}
	return &ast.ClosureLit{
		Span: ast.Span{
			StartLine: open.Line, StartCol: open.Col,
			EndLine: body.NodeSpan().EndLine, EndCol: body.NodeSpan().EndCol,
		},
		Params: params,
		Body:   bodyBlock,
		Short:  true,
	}, nil
}

// parseFuncClosure parses the full closure form `func(ParamList)
// ReturnAnnot? Block` in expression position. Cursor at `func`.
func (p *parser) parseFuncClosure() (*ast.ClosureLit, *Diag) {
	kw := p.advance() // consume 'func'
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
	var ret ast.TypeExpr
	if p.at(lexer.KindPunct, ":") {
		p.advance()
		p.skipNewlines() // ReturnAnnot: type may wrap to next line (grammar §SyntaxNewlineSuppression)
		ret, err = p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ClosureLit{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Params:     params,
		ReturnType: ret,
		Body:       body,
	}, nil
}

func (p *parser) parseCallTypeArgs() ([]ast.TypeExpr, *Diag) {
	if _, err := p.expect(lexer.KindOp, "<"); err != nil {
		return nil, err
	}
	var args []ast.TypeExpr
	for {
		p.skipNewlines()
		t, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, t)
		p.skipNewlines() // newline before `,` or closing `>` is insignificant inside `<...>`
		if p.at(lexer.KindPunct, ",") {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.KindOp, ">"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parseCallSuffix(callee ast.Expr, typeArgs []ast.TypeExpr) (*ast.Call, *Diag) {
	// Inside `( … )` arguments a `{` is unambiguously a brace
	// literal — lift any header brace-suppression.
	savedNB := p.noBrace
	p.noBrace = false
	defer func() { p.noBrace = savedNB }()
	if _, err := p.expect(lexer.KindPunct, "("); err != nil {
		return nil, err
	}
	c := &ast.Call{Callee: callee, TypeArgs: typeArgs}
	p.skipNewlines()
	if !p.at(lexer.KindPunct, ")") {
		for {
			// A leading `...` spreads a slice into a variadic parameter
			// (grammar.ebnf §Arg). A spread is only legal as the final
			// argument, so we break the loop after parsing one — a
			// following `,` then surfaces as E0112 at the closing `)`.
			if p.at(lexer.KindOp, "...") {
				dots := p.advance()
				inner, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				c.Args = append(c.Args, &ast.SpreadArg{
					Span: ast.Span{
						StartLine: dots.Line, StartCol: dots.Col,
						EndLine: inner.NodeSpan().EndLine, EndCol: inner.NodeSpan().EndCol,
					},
					Inner: inner,
				})
				p.skipNewlines()
				break
			}
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			c.Args = append(c.Args, arg)
			p.skipNewlines()
			if !p.at(lexer.KindPunct, ",") {
				break
			}
			p.advance() // consume ','
			p.skipNewlines()
		}
	}
	closeTok, err := p.expect(lexer.KindPunct, ")")
	if err != nil {
		return nil, err
	}
	c.Span = ast.Span{
		StartLine: callee.NodeSpan().StartLine, StartCol: callee.NodeSpan().StartCol,
		EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
	}
	return c, nil
}

func (p *parser) parsePrimary() (ast.Expr, *Diag) {
	t := p.peek()
	switch t.Kind {
	case lexer.KindIntLit:
		p.advance()
		v, err := parseIntLit(t.Lexeme)
		if err != nil {
			return nil, p.diag("E0109", "Malformed numeric literal", t.Line, t.Col)
		}
		return &ast.IntLitExpr{
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
		return &ast.FloatLitExpr{
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
		return &ast.StringLitExpr{
			Span:    spanFromToken(t),
			RawText: t.Lexeme,
			Value:   val,
		}, nil
	case lexer.KindStringInterp:
		p.advance()
		return p.parseStringInterp(t)
	case lexer.KindRuneLit:
		p.advance()
		val, err := decodeRuneLit(t.Lexeme)
		if err != nil {
			return nil, p.diag("E0110", "Malformed rune literal", t.Line, t.Col)
		}
		return &ast.RuneLitExpr{
			Span:    spanFromToken(t),
			RawText: t.Lexeme,
			Value:   val,
		}, nil
	case lexer.KindKeyword:
		switch t.Lexeme {
		case "true":
			p.advance()
			return &ast.BoolLitExpr{Span: spanFromToken(t), Value: true}, nil
		case "false":
			p.advance()
			return &ast.BoolLitExpr{Span: spanFromToken(t), Value: false}, nil
		case "match":
			return p.parseMatchExpr()
		case "if":
			return p.parseIfExpr()
		case "func":
			return p.parseFuncClosure()
		case "break":
			bt := p.advance()
			return &ast.BreakExpr{Span: spanFromToken(bt)}, nil
		case "continue":
			ct := p.advance()
			return &ast.ContinueExpr{Span: spanFromToken(ct)}, nil
		case "return":
			// `return` is a DivergingExpr in PrimaryExpr
			// (grammar.ebnf §DivergingExpr) — e.g. a match-arm body
			// `Err(_) => return false`. At statement position the
			// statement parser intercepts `return` first, so this arm
			// fires only in true expression position.
			return p.parseReturnExpr()
		case "this":
			p.advance()
			return &ast.ThisExpr{Span: spanFromToken(t)}, nil
		case "try":
			tk := p.advance()
			inner, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			return &ast.TryExpr{
				Span: ast.Span{
					StartLine: tk.Line, StartCol: tk.Col,
					EndLine: inner.NodeSpan().EndLine, EndCol: inner.NodeSpan().EndCol,
				},
				Inner: inner,
			}, nil
		case "scope":
			// `scope` heading a block (`<` TypeArgs / `(` parent / `{`
			// body) is a ScopeExpr; otherwise it is a ScopeRef value —
			// typically `scope.context`, with the `.` attaching as a
			// postfix suffix (grammar.ebnf §ScopeRef disambiguation).
			nxt := p.peekAhead(1)
			heads := nxt.Kind == lexer.KindOp && nxt.Lexeme == "<" ||
				nxt.Kind == lexer.KindPunct && (nxt.Lexeme == "(" || nxt.Lexeme == "{")
			if heads {
				return p.parseScopeExpr()
			}
			p.advance() // consume 'scope'
			return &ast.ScopeRef{Span: spanFromToken(t)}, nil
		case "spawn":
			return p.parseSpawnExpr()
		}
		return nil, p.diag("E0112", fmt.Sprintf("unexpected keyword %q in expression", t.Lexeme), t.Line, t.Col)
	case lexer.KindIdent:
		p.advance()
		return &ast.Ident{Span: spanFromToken(t), Name: t.Lexeme}, nil
	case lexer.KindPunct:
		if t.Lexeme == "(" {
			// `(params) => expr` short closure — disambiguated from a
			// parenthesised expr / tuple by a `=>` after the `)`.
			if p.couldBeShortClosure() {
				return p.parseShortClosure()
			}
			// Inside parens a `{` is unambiguous — lift header
			// brace-suppression for the whole `( … )`.
			savedNB := p.noBrace
			p.noBrace = false
			defer func() { p.noBrace = savedNB }()
			open := p.advance()
			p.skipNewlines()
			// `()` — the unit literal (grammar `UnitLit = "(" ")"`).
			// A `() => …` zero-param closure is already taken by the
			// couldBeShortClosure() check above, so a `)` here is unit.
			if p.at(lexer.KindPunct, ")") {
				closeTok := p.advance()
				return &ast.UnitLit{
					Span: ast.Span{
						StartLine: open.Line, StartCol: open.Col,
						EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
					},
				}, nil
			}
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			// `(e, e, ...)` is a tuple literal; `(e)` is grouping.
			if p.at(lexer.KindPunct, ",") {
				comps := []ast.Expr{e}
				for p.at(lexer.KindPunct, ",") {
					p.advance() // consume ','
					p.skipNewlines()
					if p.at(lexer.KindPunct, ")") {
						break // trailing comma
					}
					c, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					comps = append(comps, c)
					p.skipNewlines()
				}
				closeTok, err := p.expect(lexer.KindPunct, ")")
				if err != nil {
					return nil, err
				}
				return &ast.TupleLit{
					Span: ast.Span{
						StartLine: open.Line, StartCol: open.Col,
						EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
					},
					Components: comps,
				}, nil
			}
			closeTok, err := p.expect(lexer.KindPunct, ")")
			if err != nil {
				return nil, err
			}
			// Keep the grouping as a node — flattening to `e` would
			// drop the author's precedence intent (`a * (b + c)`).
			return &ast.ParenExpr{
				Span: ast.Span{
					StartLine: open.Line, StartCol: open.Col,
					EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
				},
				Inner: e,
			}, nil
		}
		if t.Lexeme == "[" {
			return p.parseSliceLit()
		}
		if t.Lexeme == "{" {
			// Block-as-expression. Only reached in true expression
			// position — control-flow headers consume their `{`
			// through parseBlock, never through parsePrimary.
			return p.parseValueBlock()
		}
	}
	if t.Kind == lexer.KindNewline {
		// The commonest form of this error: an expression split across
		// lines (a `&&`/`||`/`+` chain, a multi-line `if` condition or
		// match arm). A newline outside brackets ends the expression, so
		// a trailing binary operator has no right operand. Point at the
		// fix rather than the raw token (diagnostics.md E0112).
		return nil, p.diag("E0112",
			"a newline ends the expression here — wrap the whole expression in parentheses `(...)` to continue it across lines",
			t.Line, t.Col)
	}
	return nil, p.diag("E0112",
		fmt.Sprintf("expected an expression, got %s", describeToken(t)),
		t.Line, t.Col)
}

// ---- match expression + patterns ----

// parseMatchExpr expects the cursor at the `match` keyword.
// parseScopeExpr parses `scope<T, E>(parent?) { body }` (grammar
// ScopeExpr). The cursor is at the `scope` keyword. Type arguments
// and the optional parent-context paren are both optional at parse
// time; sema enforces the `<T, E>` arity and the v1 `E = error`
// restriction (E0407).
func (p *parser) parseScopeExpr() (*ast.ScopeExpr, *Diag) {
	kw := p.advance() // consume 'scope'
	var typeArgs []ast.TypeExpr
	if p.at(lexer.KindOp, "<") {
		ta, err := p.parseCallTypeArgs()
		if err != nil {
			return nil, err
		}
		typeArgs = ta
	}
	var parent ast.Expr
	if p.at(lexer.KindPunct, "(") {
		p.advance() // consume '('
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.KindPunct, ")"); err != nil {
			return nil, err
		}
		parent = e
	}
	// The scope's trailing expression is its success value T
	// (T-ScopeExpr), so promote it via parseValueBlock.
	body, err := p.parseValueBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ScopeExpr{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		TypeArgs: typeArgs,
		Parent:   parent,
		Body:     body,
	}, nil
}

// parseSpawnExpr parses `spawn { body }` (grammar SpawnExpr). The
// cursor is at the `spawn` keyword. Sema enforces that it appears
// inside a `scope` body (E0405).
func (p *parser) parseSpawnExpr() (*ast.SpawnExpr, *Diag) {
	kw := p.advance() // consume 'spawn'
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.SpawnExpr{
		Span: ast.Span{
			StartLine: kw.Line, StartCol: kw.Col,
			EndLine: body.Span.EndLine, EndCol: body.Span.EndCol,
		},
		Body: body,
	}, nil
}

// parseSliceLit parses either of:
//   - `[expr, expr, ...]`         — inferred-element-type form
//   - `[]T{}` or `[]T{e1, ...}`   — annotated-type form
//
// The cursor is at the leading `[`.
func (p *parser) parseSliceLit() (*ast.SliceLit, *Diag) {
	savedNB := p.noBrace
	p.noBrace = false
	defer func() { p.noBrace = savedNB }()
	openTok := p.advance() // consume '['
	// Annotated form: `[` immediately followed by `]` is the
	// SliceType prefix; the following `{...}` carries the items.
	if p.at(lexer.KindPunct, "]") {
		p.advance() // consume ']'
		// Element type follows.
		elem, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.KindPunct, "{"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		var items []ast.Expr
		for !p.at(lexer.KindPunct, "}") {
			it, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			items = append(items, it)
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
		return &ast.SliceLit{
			Span: ast.Span{
				StartLine: openTok.Line, StartCol: openTok.Col,
				EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
			},
			ElemType: elem,
			Items:    items,
		}, nil
	}
	// Inferred form: `[e1, e2, ...]`. Newlines after `[`, between
	// items, and before a trailing-comma `]` are suppressed (grammar
	// §SyntaxNewlineSuppression — `[...]` region).
	p.skipNewlines()
	var items []ast.Expr
	for !p.at(lexer.KindPunct, "]") {
		it, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		items = append(items, it)
		p.skipNewlines()
		if !p.at(lexer.KindPunct, ",") {
			break
		}
		p.advance() // consume ','
		p.skipNewlines()
	}
	closeTok, err := p.expect(lexer.KindPunct, "]")
	if err != nil {
		return nil, err
	}
	return &ast.SliceLit{
		Span: ast.Span{
			StartLine: openTok.Line, StartCol: openTok.Col,
			EndLine: closeTok.Line, EndCol: closeTok.Col + 1,
		},
		Items: items,
	}, nil
}

// parseStringInterp turns a KindStringInterp token `"a ${e} b"` into a
// StringInterpExpr (grammar.ebnf §StringInterp): it splits the raw lexeme
// into literal segments and hole sources (brace-depth aware — the lexer
// guaranteed each `${` has a matching `}`), decodes the escapes in the
// literals, and sub-parses each hole as one expression. Parts has one
// more element than Holes (the segments around the holes).
func (p *parser) parseStringInterp(t lexer.Token) (ast.Expr, *Diag) {
	s := t.Lexeme
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return nil, p.diag("E0110", "Malformed string literal", t.Line, t.Col)
	}
	inner := s[1 : len(s)-1]
	var parts []string
	var holes []ast.Expr
	var lit strings.Builder
	flushLit := func() *Diag {
		dec, err := decodeStringSegment(lit.String())
		if err != nil {
			return p.diag("E0110", "Malformed escape sequence", t.Line, t.Col)
		}
		parts = append(parts, dec)
		lit.Reset()
		return nil
	}
	for i := 0; i < len(inner); {
		if inner[i] == '$' && i+1 < len(inner) && inner[i+1] == '{' {
			if d := flushLit(); d != nil {
				return nil, d
			}
			i += 2
			depth := 1
			start := i
			for i < len(inner) && depth > 0 {
				switch inner[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth == 0 {
					break
				}
				i++
			}
			hole, d := p.parseHoleExpr(inner[start:i], start, t)
			if d != nil {
				return nil, d
			}
			holes = append(holes, hole)
			i++ // consume the closing '}'
			continue
		}
		// Preserve an escape pair verbatim so the decoder resolves it.
		if inner[i] == '\\' && i+1 < len(inner) {
			lit.WriteByte(inner[i])
			lit.WriteByte(inner[i+1])
			i += 2
			continue
		}
		lit.WriteByte(inner[i])
		i++
	}
	if d := flushLit(); d != nil {
		return nil, d
	}
	return &ast.StringInterpExpr{
		Span:  spanFromToken(t),
		Parts: parts,
		Holes: holes,
	}, nil
}

// parseHoleExpr sub-lexes and parses one interpolation hole source as a
// single expression; an empty hole or one that leaves trailing tokens is
// an E0112. holeByteOff is the hole source's byte offset within the
// string body, used to shift the sub-lexed token coordinates back onto
// the enclosing `.aril` line/column so a later sema diagnostic on a hole
// node points at the real source (D10). A hole never spans a newline (the
// lexer rejects a raw newline in a string), so every hole token is on the
// string token's line.
func (p *parser) parseHoleExpr(src string, holeByteOff int, t lexer.Token) (ast.Expr, *Diag) {
	toks, lerr := lexer.Lex(src)
	if lerr != nil {
		return nil, p.diag("E0112", "malformed expression in interpolation hole", t.Line, t.Col)
	}
	// The hole's first source char sits at column t.Col+1 (past the opening
	// quote) + holeByteOff; a sub-token at its own 1-based column c maps to
	// baseCol+(c-1). ASCII-column approximation, adequate for v1.
	baseCol := t.Col + 1 + holeByteOff
	for i := range toks {
		if toks[i].Kind == lexer.KindEOF {
			continue
		}
		toks[i].Line = t.Line
		toks[i].Col = baseCol + (toks[i].Col - 1)
	}
	sub := &parser{toks: suppressBracketNewlines(toks), file: p.file}
	e, err := sub.parseExpr()
	if err != nil {
		return nil, p.diag("E0112", "an interpolation hole must contain a single expression", t.Line, t.Col)
	}
	for sub.peek().Kind == lexer.KindNewline {
		sub.advance()
	}
	if sub.peek().Kind != lexer.KindEOF {
		return nil, p.diag("E0112", "an interpolation hole must contain a single expression", t.Line, t.Col)
	}
	return e, nil
}
