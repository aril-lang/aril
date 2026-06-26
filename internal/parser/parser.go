package parser

import (
	"fmt"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// Diag is a parser-level diagnostic with the same shape as
// lexer.Diag (canonical file:line:col format).
type Diag struct {
	File    string
	Code    string
	Message string
	Line    int
	Col     int
}

func (d *Diag) Error() string {
	if d.File == "" {
		return fmt.Sprintf("%d:%d: error[%s]: %s", d.Line, d.Col, d.Code, d.Message)
	}
	return fmt.Sprintf("%s:%d:%d: error[%s]: %s", d.File, d.Line, d.Col, d.Code, d.Message)
}

// Parse takes a token stream (from lexer.Lex) and returns a *File.
// The first diagnostic encountered halts parsing.
func Parse(toks []lexer.Token) (*ast.File, *Diag) {
	return ParseFile(toks, "")
}

// ParseFile is Parse but tags diagnostics with the source filename.
func ParseFile(toks []lexer.Token, file string) (*ast.File, *Diag) {
	p := &parser{toks: suppressBracketNewlines(toks), file: file}
	return p.parseFile()
}

// suppressBracketNewlines drops Newline tokens that fall inside an open
// `(...)` or `[...]` region — per grammar.ebnf §SyntaxNewlineSuppression
// (and the lexical-part note: the lexer produces every Newline; the
// parser suppresses those inside open brackets). A `{...}` block is NOT
// a suppression region — newlines inside it are statement separators —
// and record/map/set/stack literals (also `{...}`) suppress between
// entries via their own parsers, not here. So a `{` saves the current
// bracket depth and resets it to 0; the matching `}` restores it.
// `<...>` type-argument lists are handled in the type-arg parser, not
// here: a bare `<` is ambiguous with the less-than operator at the
// token level. All non-Newline tokens (and their coordinates) are
// preserved, so spans are unaffected.
func suppressBracketNewlines(toks []lexer.Token) []lexer.Token {
	out := make([]lexer.Token, 0, len(toks))
	depth := 0
	var stack []int // bracket depths saved at each enclosing `{`
	for _, t := range toks {
		if t.Kind == lexer.KindNewline && depth > 0 {
			continue
		}
		out = append(out, t)
		if t.Kind != lexer.KindPunct {
			continue
		}
		switch t.Lexeme {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case "{":
			stack = append(stack, depth)
			depth = 0
		case "}":
			if len(stack) > 0 {
				depth = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}

type parser struct {
	toks []lexer.Token
	pos  int
	file string
	// noBrace suppresses brace-literal parsing of a trailing `{` so
	// control-flow headers (`if cond {`, `for x in it {`, `while
	// cond {`, `match subj {`) read the `{` as their block rather
	// than as `cond { … }` brace literal. Set while parsing a header
	// expression; reset inside any delimited sub-expression where a
	// `{` is unambiguous (parens, call args, brackets, brace body).
	noBrace bool
}

// withNoBrace runs fn (a header-expression parse) with brace-literal
// suppression on, restoring noBrace after — so a trailing `{` reads as
// the control-flow block, not a `cond { … }` brace literal. Delimited
// contexts (parens, call args, brackets, brace bodies) instead use the
// inline `defer` form to re-enable braces.
func (p *parser) withNoBrace(fn func() (ast.Expr, *Diag)) (ast.Expr, *Diag) {
	saved := p.noBrace
	p.noBrace = true
	e, err := fn()
	p.noBrace = saved
	return e, err
}

// ---- token cursor helpers ----

func (p *parser) peek() lexer.Token {
	if p.pos >= len(p.toks) {
		// Defensive: lexer always appends EOF so this shouldn't
		// happen, but emit a synthetic EOF if it does.
		return lexer.Token{Kind: lexer.KindEOF}
	}
	return p.toks[p.pos]
}

// peekAhead returns the token n positions past the cursor (n == 0 is
// peek()), or a synthetic EOF past the end.
func (p *parser) peekAhead(n int) lexer.Token {
	if p.pos+n >= len(p.toks) {
		return lexer.Token{Kind: lexer.KindEOF}
	}
	return p.toks[p.pos+n]
}

// peekPastNewlines returns the first token at or after the cursor that
// is not a Newline (a synthetic EOF past the end). Used for leading-dot
// method-chain continuation, where a `.` on the next line continues the
// chain but any other token terminates the expression.
func (p *parser) peekPastNewlines() lexer.Token {
	for i := p.pos; i < len(p.toks); i++ {
		if p.toks[i].Kind != lexer.KindNewline {
			return p.toks[i]
		}
	}
	return lexer.Token{Kind: lexer.KindEOF}
}

func (p *parser) at(k lexer.Kind, lex ...string) bool {
	t := p.peek()
	if t.Kind != k {
		return false
	}
	if len(lex) == 0 {
		return true
	}
	for _, want := range lex {
		if t.Lexeme == want {
			return true
		}
	}
	return false
}

// skipNewlines consumes runs of Newline tokens. Newlines are
// statement separators, but several positions don't care about
// them (after an open brace, before a closing one, between
// tokens of a single expression continued on the next line via
// open brackets, …). PR-B's parser is lenient: newlines are
// skipped at most positions, treated as a separator only between
// statements inside a Block.
func (p *parser) skipNewlines() {
	for p.at(lexer.KindNewline) {
		p.pos++
	}
}

// skipStmtSeps consumes statement terminators between block
// statements — newlines and `;` in any interleaving, per grammar.ebnf
// Stmtterm = Newline+ | ";" Newline*.
func (p *parser) skipStmtSeps() {
	for p.at(lexer.KindNewline) || p.at(lexer.KindPunct, ";") {
		p.pos++
	}
}

func (p *parser) advance() lexer.Token {
	t := p.peek()
	p.pos++
	return t
}

// expect consumes a token of kind k (and matching lexeme, if given)
// or returns a diagnostic.
func (p *parser) expect(k lexer.Kind, lex string) (lexer.Token, *Diag) {
	t := p.peek()
	if t.Kind != k || (lex != "" && t.Lexeme != lex) {
		return t, p.diag("E0112", fmt.Sprintf("expected %s %q, got %s %q",
			k, lex, t.Kind, t.Lexeme), t.Line, t.Col)
	}
	p.pos++
	return t, nil
}

func (p *parser) diag(code, msg string, line, col int) *Diag {
	return &Diag{File: p.file, Code: code, Message: msg, Line: line, Col: col}
}

// atContractBlock reports whether the cursor is at a top-level
// separable contract surface: `contract <name> {` or `channel
// <name> {`. `contract`/`channel` are not reserved keywords, so the
// claim is positional — at the top level only declarations are legal
// and an identifier there is otherwise an error, making this shape
// unambiguous. The trailing `{` is required: a lone `contract`/
// `channel` identifier falls through to the normal decl error.
func (p *parser) atContractBlock() bool {
	if !p.at(lexer.KindIdent, "contract", "channel") {
		return false
	}
	return p.peekAhead(1).Kind == lexer.KindIdent &&
		p.peekAhead(2).Kind == lexer.KindPunct && p.peekAhead(2).Lexeme == "{"
}

// ---- file ----

func (p *parser) parseFile() (*ast.File, *Diag) {
	startLine, startCol := 1, 1
	f := &ast.File{}

	p.skipNewlines()
	// Imports first.
	for p.at(lexer.KindKeyword, "import") {
		im, err := p.parseImport()
		if err != nil {
			return nil, err
		}
		f.Imports = append(f.Imports, im)
		p.skipNewlines()
	}
	// Then declarations.
	for !p.at(lexer.KindEOF) {
		// Separable contract surface. Both forms parse into side tables
		// (File.Contracts / File.Channels) kept *out* of Decls, so codegen
		// and sema (which iterate Decls) lower byte-identically until the
		// contract passes consume them. `contract <name> { … }` (RFC-0006
		// value/state) → ContractDecl; `channel <name> { … }` (RFC-0007
		// trace contracts) → ChannelDecl.
		if p.atContractBlock() {
			if p.at(lexer.KindIdent, "contract") {
				cd, err := p.parseContractDecl()
				if err != nil {
					return nil, err
				}
				f.Contracts = append(f.Contracts, cd)
			} else {
				chd, err := p.parseChannelDecl()
				if err != nil {
					return nil, err
				}
				f.Channels = append(f.Channels, chd)
			}
			p.skipNewlines()
			continue
		}
		d, err := p.parseDecl()
		if err != nil {
			return nil, err
		}
		f.Decls = append(f.Decls, d)
		p.skipNewlines()
	}
	if len(f.Decls) == 0 {
		return nil, p.diag("E0112", "File has no declarations", 1, 1)
	}
	eof := p.peek()
	f.Span = ast.Span{
		StartLine: startLine, StartCol: startCol,
		EndLine: eof.Line, EndCol: eof.Col,
	}
	return f, nil
}
