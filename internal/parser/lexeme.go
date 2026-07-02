package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/lexer"
)

// ---- helpers ----

func spanFromToken(t lexer.Token) ast.Span {
	return ast.Span{
		StartLine: t.Line, StartCol: t.Col,
		EndLine: t.Line, EndCol: t.Col + tokenWidth(t),
	}
}

func tokenWidth(t lexer.Token) int {
	// Char-counted (not byte-counted) token width — matches the
	// lexer's column-counting convention (utf8-aware).
	return utf8.RuneCountInString(t.Lexeme)
}

func parseIntLit(s string) (int64, error) {
	// Strip "_" separators (grammar.ebnf admits them anywhere in
	// the literal body); pick base from prefix.
	clean := strings.ReplaceAll(s, "_", "")
	if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
		return strconv.ParseInt(clean[2:], 16, 64)
	}
	if strings.HasPrefix(clean, "0o") || strings.HasPrefix(clean, "0O") {
		return strconv.ParseInt(clean[2:], 8, 64)
	}
	if strings.HasPrefix(clean, "0b") || strings.HasPrefix(clean, "0B") {
		return strconv.ParseInt(clean[2:], 2, 64)
	}
	return strconv.ParseInt(clean, 10, 64)
}

// decodeRuneLit converts a single-quoted rune literal lexeme
// (`'a'`, `'\n'`, `'\\'`) to its decoded code point. Delegates
// to Go's strconv.UnquoteChar so all standard Go rune escapes
// (`\n`, `\t`, `\xNN`, `\uNNNN`, `\UNNNNNNNN`) are accepted.
func decodeRuneLit(s string) (int32, error) {
	if len(s) < 3 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return 0, fmt.Errorf("not a rune literal")
	}
	r, _, _, err := strconv.UnquoteChar(s[1:len(s)-1], '\'')
	if err != nil {
		return 0, err
	}
	return int32(r), nil
}

// decodeStringLit converts a lexer-token lexeme `"hello\n"` to the
// decoded value `hello<LF>`.
func decodeStringLit(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("not a string literal")
	}
	return decodeStringSegment(s[1 : len(s)-1])
}

// decodeStringSegment resolves the v1 escape sequences in an unquoted
// string body — shared by whole string literals and the literal segments
// of an interpolated string (grammar.ebnf §EscapeChar / §StringInterp).
func decodeStringSegment(inner string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(inner); {
		c := inner[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(inner) {
			return "", fmt.Errorf("trailing backslash")
		}
		esc := inner[i+1]
		switch esc {
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '"':
			b.WriteByte('"')
			i += 2
		case '\'':
			b.WriteByte('\'')
			i += 2
		case '0':
			b.WriteByte(0)
			i += 2
		case 'x':
			if i+3 >= len(inner) {
				return "", fmt.Errorf("short \\x escape")
			}
			n, err := strconv.ParseInt(inner[i+2:i+4], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(n))
			i += 4
		case 'u':
			if i+5 >= len(inner) {
				return "", fmt.Errorf("short \\u escape")
			}
			n, err := strconv.ParseInt(inner[i+2:i+6], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(n))
			i += 6
		default:
			return "", fmt.Errorf("unknown escape \\%c", esc)
		}
	}
	return b.String(), nil
}
