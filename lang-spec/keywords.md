# Reserved words, operators, and punctuation

This file is the **canonical, exhaustive** list of Aril's reserved
lexical surface. The lexer is generated from it; if a token does not
appear here, the lexer does not produce it and the parser does not
recognise it. Prose in `../docs/language-spec.md` mirrors this list;
on a disagreement, this file wins.

Updates require a paired update to the test corpus
(`../tests/lexer/`), the grammar (`grammar.ebnf`), and any affected
examples.

## Reserved keywords

These identifiers are **always reserved** — they cannot be used as
binding names, parameter names, field names, type names, or method
names.

### Declaration and control flow

```
import    type      class      interface
implements          extends    static
func      let       const      var        if         else
for       in        while      return
match     try       defer      spawn      scope      select
break     continue  extern
```

`extern` introduces a foreign-binding declaration (`extern type`,
`extern func`, `extern impl`) — the Go FFI surface in `ffi.md`. It is
the only new hard keyword the FFI adds; `impl` and `go` stay
contextual (below).

`const` is a surface alias for `let`. The two produce identical
AST nodes and the same lowering; `const` exists so the user can
spell "this binding is intended as a named constant" without
the spec growing a separate semantic category.

`newtype` is reserved-in-principle for nominal newtypes (D11 open
issue — the v1 working placeholder is `newtype X = T`), but does not
appear in the corpus yet. It returns to this list with the PR that
ships its concrete syntax.

### Type literals

```
true      false     unit
```

`true` and `false` are the two `bool` values. `unit` is the type with
exactly one value, also written `()`.

### Receiver

```
this
```

`this` is the explicit form of the implicit receiver inside an
instance method (see `../docs/language-spec.md` §Classes). Outside class
methods, `this` is a syntax error.

## Contextual keywords

These tokens are keywords **only in specific positions**; outside
those positions they are ordinary identifiers.

| Token | Where it's a keyword | Elsewhere |
|---|---|---|
| `_`     | `let _`, `var _`, `for _ in`, function-parameter, `match` patterns | identifier |
| `case`  | inside `select { ... }` | identifier |
| `default` | inside `select { ... }` | identifier |
| `impl`  | immediately after `extern` (`extern impl T { … }`) | identifier |
| `go`    | as the attribute head in `@go("…")` (a `@` token followed by `go`) | identifier |
| `contract` | at the head of a top-level block (`contract <name> {`), RFC-0006 | identifier |
| `channel`  | at the head of a top-level block (`channel <name> {`), RFC-0007 | identifier |

`contract` / `channel` are contextual: a keyword only in the positional
`<word> <name> {` top-level shape (where an identifier would otherwise be a
declaration error), an ordinary identifier everywhere else. The block grammar
(`grammar.ebnf` §ContractBlock) is the separable contract surface; during the
CONTRACTS-IMPL bootstrap the body is parsed-and-skipped (the `--contracts=off`
ignore level) until the enforcement pipeline lands.

## Built-in identifiers (predeclared, NOT keywords)

These are predeclared in the top-level scope. Full signatures live in
`builtins.md`.

The **type** names below are **reserved**: a `type` / `class` /
`interface` / `extern type` declaration may not redeclare one (E0118 —
see `name-resolution.md` §Reserved type names). Value-level shadowing of
a predeclared identifier by a local or parameter is permitted (though bad
style) and is governed separately by the shadow diagnostics.

- Types: `bool`, `int`, `int8`..`int64`, `uint`..`uint64`,
  `float32`, `float64`, `byte`, `rune`, `string`, `Any`,
  `Dynamic`, `error`.
- Generic types: `Result`, `Option`, `Map`, `Set`, `Stack`,
  `Channel`, `SendChan`, `RecvChan`.
- Variant constructors: `Ok`, `Err`, `Some`, `None`.
- Functions: `panic`, `error`, `refEq`, `makeChannel`, `makeSlice`.
- Conversion: `int`, `int64`, ..., `float64`, `byte`, `rune`, `string`
  also act as conversion functions (`int(x)`, `float64(n)`, ...).

`Any` and `Dynamic` are deliberately separate:

- `Any` is the **binding-boundary** escape type — it appears only
  in variadic stdlib binding signatures (e.g., `fmt.println(args:
  ...Any)`). User-authored Aril code does not introduce `Any`-typed
  parameters, fields, or return types. See `type-system.md` §`Any`.
- `Dynamic` is the **user-facing** runtime-erased wrapper used by
  the reflection API. Introduced only via implicit widening at
  `reflect.*` parameter sites or explicit `reflect.box`; eliminated
  only via `reflect.unbox<T>`. See `type-system.md` §`Dynamic`
  (paired edit in PR-S3).

Keeping the two names distinct is a deliberate cultural-line
measure: `Any` reads as "internal FFI", `Dynamic` reads as "runtime
box". The compiler should never silently promote one to the other.

## Operators

### Binary

| Operator | Meaning |
|---|---|
| `+`  | numeric add / string concat |
| `-`  | numeric subtract |
| `*`  | numeric multiply |
| `/`  | numeric divide (integer or float per operands) |
| `%`  | numeric remainder |
| `==` | equality |
| `!=` | inequality |
| `<`  | less-than (comparable primitives only) |
| `<=` | less-than-or-equal |
| `>`  | greater-than |
| `>=` | greater-than-or-equal |
| `&&` | logical AND, short-circuit |
| `\|\|` | logical OR, short-circuit |
| `\|` | sum-type variant separator (`type X = \| A \| B(...)`) and pattern alternative inside `match` (`'(' \| '[' \| '{' => ...`). Not an expression-level operator |

### Unary

| Operator | Meaning |
|---|---|
| `!` | logical NOT |
| `-` | numeric negation |

### Assignment and binding

| Operator | Meaning |
|---|---|
| `=`  | initialiser in a `let` / `var` declaration; assignment to a `var` binding or `var` field; right-hand side of a record / map literal field (`k: v` form). Not a comparison operator |
| `let` / `var` | binding declaration (keywords above; not operators) |

There is no compound assignment in v1 (`+=`, `-=`, ... are not
recognised). Write `x = x + 1` explicitly.

### Receive operator

| Operator | Meaning |
|---|---|
| `<-ch` | channel receive — **only** legal inside a `select` case |

## Punctuation and structural symbols

The table below describes the *role* of each symbol; the **token
kind** (`Punct` vs `Op`) is fixed by `grammar.ebnf` — punctuation
brackets and separators are `Punct`; the arrows (`->`, `=>`),
ranges (`..`, `..=`), and variadic (`...`) are `Op` tokens. On
disagreement, `grammar.ebnf` wins (D17).

```
(  )       Punct — parens; grouping, parameter list, call args
[  ]       Punct — brackets; slice literal, slice indexing/slicing
{  }       Punct — braces; block, record literal, class body, scope body
<  >       Op    — angle brackets / comparison; generic args resolved by parser
.          Punct — field access, method call, tuple-position access (t.0)
,          Punct — separator in lists / args / tuples
;          Punct — statement terminator (optional — newline normally suffices)
:          Punct — type annotation, record / map field, slice slicing
->         Op    — return-type arrow — **reserved**; not produced in v1
=>         Op    — arm separator in `match` and short-closure literals
..         Op    — half-open range (`a..b`)
..=        Op    — inclusive range (`a..=b`)
...        Op    — variadic parameter / spread (e.g. `args: ...Any`)
@          Punct — foreign-binding attribute head (`@go("…")`, ffi.md); no other v1 production accepts it
```

## Lexical conflict resolution

- **`<` and `>` as comparison vs generic brackets** — the parser uses
  one-token lookahead and context: in type-expression position and
  immediately after a type / function name, `<` opens a generic
  argument list; in expression position it is comparison. Ambiguous
  cases (`f<a, b>(c)`) follow Go's "if the next non-whitespace after
  `>` is `(` or a left-paren-leading expression, treat as generic
  arguments" rule.
- **`..` and `..=` versus member access** — both are punctuated forms
  (`a..b`, `a..=b`); the lexer is greedy and produces the longest
  match. Member access stays `.` between non-digit identifiers; tuple
  position `t.0` is `.` followed by an integer literal.
- **`-` unary vs binary** — disambiguation by parser context; the
  lexer produces a single `-` token either way.
- **`!` unary only** — there is no postfix `!`; `!a` is unary
  negation, never a method invocation.

## What is NOT a keyword (deliberately)

The following words look reserved in other languages but are
ordinary identifiers in Aril; user code is free to shadow them
(though bad style):

- `goto`, `do`, `enum`, `struct`, `pub`, `fn`, `async`, `await`,
  `yield`, `throw`, `catch`, `finally`, `with`, `assert`, `nil`,
  `null`, `undefined`, `new`, `delete`, `self`, `super`.

`async`/`await`/`yield` are explicitly cut by D7. `nil`/`null`/
`undefined` are absent because `Option<T>` is the nullable type
(D2). `goto`, `do`, `super`, `new`, `delete` have no role in the
v1 surface.
