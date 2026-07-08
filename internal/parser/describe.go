package parser

import (
	"fmt"

	"github.com/aril-lang/aril/internal/lexer"
)

// User-facing token spelling for parser diagnostics (E0112). The internal
// lexer.Kind names (Punct, Keyword, Ident, …) are the test-contract.md §TOKENS
// contract for the *lexer* — they must never surface in a user-facing message,
// which speaks in the source spelling (`)`, `in`) or a token class ("an
// identifier"). See diagnostics.md §E0112.

// spellLiteral renders a token the grammar requires *literally* (a punctuation,
// operator, or keyword) as its backticked source spelling — `)`, `in`, `{`.
func spellLiteral(lex string) string {
	return fmt.Sprintf("`%s`", lex)
}

// describeKind names a token *class*, for an expected position that admits any
// token of a kind rather than one literal spelling (e.g. an identifier name).
func describeKind(k lexer.Kind) string {
	switch k {
	case lexer.KindIdent:
		return "an identifier"
	case lexer.KindKeyword:
		return "a keyword"
	case lexer.KindIntLit:
		return "an integer literal"
	case lexer.KindFloatLit:
		return "a floating-point literal"
	case lexer.KindStringLit, lexer.KindStringInterp:
		return "a string literal"
	case lexer.KindRuneLit:
		return "a character literal"
	case lexer.KindOp:
		return "an operator"
	case lexer.KindPunct:
		return "punctuation"
	case lexer.KindNewline:
		return "a line break"
	case lexer.KindEOF:
		return "end of file"
	default:
		return "a token"
	}
}

// describeToken renders the token actually found, for the "got …" half of a
// diagnostic: the concrete lexeme in backticks, or a phrase for the token
// classes that carry no printable lexeme (a newline, end of file).
func describeToken(t lexer.Token) string {
	switch t.Kind {
	case lexer.KindEOF:
		return "end of file"
	case lexer.KindNewline:
		return "a line break"
	}
	if t.Lexeme == "" {
		return describeKind(t.Kind)
	}
	return fmt.Sprintf("`%s`", t.Lexeme)
}
