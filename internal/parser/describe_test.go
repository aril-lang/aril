package parser

import (
	"testing"

	"github.com/aril-lang/aril/internal/lexer"
)

// The E0112 humanizer must render tokens in user-facing source spelling, never
// the internal lexer.Kind names (Punct, Keyword, …) — those are the
// test-contract.md §TOKENS contract for the lexer, not a user message. See
// diagnostics.md §E0112.

func TestSpellLiteralBackticks(t *testing.T) {
	for _, lex := range []string{")", "in", "{", "("} {
		if got, want := spellLiteral(lex), "`"+lex+"`"; got != want {
			t.Errorf("spellLiteral(%q) = %q, want %q", lex, got, want)
		}
	}
}

func TestDescribeKindPhrases(t *testing.T) {
	cases := map[lexer.Kind]string{
		lexer.KindIdent:   "an identifier",
		lexer.KindIntLit:  "an integer literal",
		lexer.KindNewline: "a line break",
		lexer.KindEOF:     "end of file",
	}
	for k, want := range cases {
		if got := describeKind(k); got != want {
			t.Errorf("describeKind(%v) = %q, want %q", k, got, want)
		}
	}
}

func TestDescribeToken(t *testing.T) {
	cases := []struct {
		tok  lexer.Token
		want string
	}{
		// A concrete lexeme is shown backticked, whatever its kind.
		{lexer.Token{Kind: lexer.KindPunct, Lexeme: ")"}, "`)`"},
		{lexer.Token{Kind: lexer.KindKeyword, Lexeme: "var"}, "`var`"},
		{lexer.Token{Kind: lexer.KindIdent, Lexeme: "cause"}, "`cause`"},
		{lexer.Token{Kind: lexer.KindIntLit, Lexeme: "1"}, "`1`"},
		// The lexeme-less classes get a phrase, not an empty pair of backticks.
		{lexer.Token{Kind: lexer.KindNewline, Lexeme: ""}, "a line break"},
		{lexer.Token{Kind: lexer.KindEOF, Lexeme: ""}, "end of file"},
	}
	for _, c := range cases {
		if got := describeToken(c.tok); got != c.want {
			t.Errorf("describeToken(%+v) = %q, want %q", c.tok, got, c.want)
		}
	}
}
