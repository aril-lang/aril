// Package parser turns an Aril token stream into an AST.
//
// Contract: lang-spec/grammar.ebnf (syntactic part) and lang-spec/ast.md.
//
// The package is one cohesive unit split across files by responsibility:
//
//   - parser.go    — the parser harness: the parser/Diag types, the
//     ParseFile entry point, bracket-newline suppression, and the token
//     cursor (peek/at/advance/expect/skip/diag).
//   - decl.go      — top-level declarations: imports, let, type (sum /
//     record), class, interface, func, and the param/type-param lists.
//   - extern.go    — the FFI extern surface (extern type/func/impl/method/
//     field and the @go attribute).
//   - type_expr.go — type-expression parsing.
//   - stmt.go      — statements and control flow (blocks, if/for/while,
//     select, defer, let/var, assignment, return).
//   - expr.go      — expressions: operator precedence, unary, postfix,
//     calls, closures, brace literals, slice literals, primaries, and
//     the scope/spawn expressions.
//   - match.go     — match expressions and pattern parsing.
//   - lexeme.go    — token→value decoders (int/rune/string literals) and
//     span helpers.
package parser
