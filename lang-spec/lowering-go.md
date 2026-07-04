# Lowering — Aril IR → Go

The contract for codegen: how the post-desugaring Aril IR
(`desugaring.md`) becomes Go source. The output is Go that
`go build` accepts; it is **not** a human-reading goal —
generated Go is an intermediate representation (D1, hard
constraint).

This file is the contract for the lowering pass that runs after
desugaring and produces `.go` files in the output tree.

**Authority.** This file is the contract. Cross-refs to
`desugaring.md` (input IR), `builtins.md` (semantic
signatures of built-in operations), `type-system.md` (typing
guarantees codegen relies on), and `test-contract.md` (the
canonical `GO` / `STDOUT` / `EXIT` sections in fixtures).

## Output tree shape

The runtime is the **`arilrt`** package — the single source of truth
for the helpers codegen depends on (Option/Result and their boundary
lifts, the Map/Set/Stack containers, the stdin scan helpers, the
structured-concurrency group, the JSON round-trip, and the reflection
layer). Codegen emits one of two forms, selectable per command:

- **Vendored** (default for `build` / `run`): `main.go` imports the
  `arilrt` package (qualified `arilrt.Option`, `arilrt.NewMap`, …) and
  the build harness copies the runtime sources beside it. This is
  version-locked by construction — the compiler binary embeds the exact
  runtime it emits.

  ```
  <out>/main.go                  // generated user package; imports <module>/arilrt
  <out>/arilrt/*.go              // package arilrt — runtime helpers (copied
                                 //  from the compiler's embedded sources)
  <out>/go.mod                   // module declaration; toolchain `go 1.22`
  ```

- **Inline single-file** (`--inline-runtime`; default for `emit`):
  codegen inlines only the *used* parts of the runtime into `main.go`
  with bare names, keeping `emit` a self-contained `.go` artifact. No
  `arilrt/` directory is emitted.

The reflect layer carries one wrinkle: its fixed runtime lives in
`arilrt` (vendored) or inline (single-file), but the *per-program*
descriptors and field accessors are always emitted into `main.go` (they
name user types), qualifying the arilrt-provided types under vendored
mode. The scan tuple helpers (`Scan2`/`Scan3`) likewise stay in `main`
in both modes — their anonymous tuple-payload struct must be declared
there so a user `Ok((a, b))` destructure can read its unexported fields.

The `arilrt` package uses a **plain Go package name** — a leading `_`
would make it invisible to `go build`. Collision protection is at the
**Aril source level**: the user-source identifier `arilrt` is reserved
(see §Identifier encoding, E0107 applies); a user type whose name
shadows a runtime type (e.g. a user `type Dynamic`) keeps its own
spelling and is *not* qualified. The full `<out>` location is set by the
`aril build` CLI; this file fixes only the relative layout.

## Identifier encoding

```
Aril identifier              Go identifier
─────────────────────────────────────────────────────
foo                          foo                       (no change)
fooBar                       fooBar
$aril_NN                     _aril_NN                  (fresh locals — see desugaring.md)
goReservedWord (e.g. type)   aril_type, aril_func, …   (`aril_` prefix to escape)
camelCase                    camelCase
SnakeCase                    SnakeCase
```

**Reserved user-source prefix.** To guarantee no collision
between user-source identifiers and codegen's `_aril_NN` fresh
names, the lexer rejects any user-source identifier whose
**first six characters** are `_aril_` (case-sensitive). Emits
**E0107 Reserved identifier prefix** — a hard error at lex time.
This is a paired edit with `grammar.ebnf` (lexical
`Ident` production); a user-source identifier starting with
`_aril_` is grammar-illegal.

The Go reserved-word list as of Go 1.22 is hard-coded into the
codegen pass: `break case chan const continue default defer
else fallthrough for func go goto if import interface map
package range return select struct switch type var`. Any Aril
identifier matching this list gets a `aril_` prefix at every
use site in generated Go.

Exported visibility — Aril has no `pub` qualifier; every
top-level decl is package-visible. Codegen capitalises the first
letter of top-level declarations when they need to be visible
from a sibling Go package (cross-file imports inside a Aril
project), and lower-cases otherwise. Since v1 has single-package
projects, all decls stay lower-cased; the algorithm is here for
future expansion.

## Record / struct field lowering

A nominal record (`type X = { f: T }`) and a class lower to a named
Go `struct`. Each Aril field `f` lowers to an **exported** Go field
(`exportFieldName`: first letter capitalised; a leading non-letter
gets an `X` prefix) carrying a `` `json:"f"` `` tag that pins the JSON
key to the verbatim Aril name:

```
type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}
```

This is **independent** of the top-level exported-visibility algorithm
above (which governs type/func *decl* names and stays lower-cased in
single-package v1). Struct fields export *unconditionally*: Go's
`encoding/json` reflects from outside package main, so an unexported
field is invisible to marshal/unmarshal — exporting is what makes JSON
round-trip work. The `json` tag keeps field-name == JSON-key
(binding-surface.md §encoding/json) so the capitalised Go spelling is
invisible at the Aril-source and wire levels.

Every field *site* follows the same spelling: the struct decl, the
record/class brace literal (`Config{Host: …}`), value-position field
access (`cfg.Host`), the implicit-receiver bare field (`this.x` →
`t.X`), and the reflection field accessors. **Method** selectors are
*not* exported — they keep their lowercase Go spelling (reachable
within package main), so field-access and method-call lowering use
distinct spelling functions (`goFieldName` vs `goMethodName`). The
package-namespace exemption (`os.args` → `os.Args` keeps its binding
rename, not the export path) is gated on the receiver's sema symbol
being a builtin module, not on its spelling — a local value that
shadows a package name still exports its fields.

A func-typed *data field* may itself be called (`handler.fn(x)`); the
call site spells it with the exported **field** form (`handler.Fn(x)`),
not the method form — the two lowering paths are kept distinct so this
does not collapse to a method-name spelling.

**Known limitation — non-injective export.** `exportFieldName` is not
injective: two field names differing only in first letter case
(`port` / `Port`) both map to `Port`, and `_x` / `X_x` both map to
`X_x`. Such a record produces a duplicate Go struct field — a *loud*
`go build` failure, never a silent miscompile. v1 leaves this
unguarded (no corpus shape hits it); a sema diagnostic on colliding
exported forms is the eventual fix.

Tuple fields keep their positional unexported spelling (`_0`, `_1`):
they are the anonymous-struct tuple representation, not nominal record
fields, and no v1 program serialises a tuple to JSON.

## Primitive type lowering

| Aril | Go |
|---|---|
| `bool` | `bool` |
| `int` | `int` |
| `int8`..`int64` | `int8`..`int64` |
| `uint`..`uint64` | `uint`..`uint64` |
| `byte` | `byte` (= `uint8`) |
| `rune` | `rune` (= `int32`) |
| `float32`, `float64` | `float32`, `float64` |
| `string` | `string` |
| `unit` | `struct{}` (the zero-byte type) |
| `Never` | `struct{}` (no value ever flows; codegen errors if encountered) |
| `Any` | `interface{}` (also written `any` in Go 1.18+) |

`unit` values (the `()` literal) emit as `arilrt.Unit` — a
package-level variable of type `struct{}` — so `unit`-typed
expressions are non-empty Go expressions.

## String interpolation

A `StringInterpExpr` (`"a ${e1} b ${e2} c"`) lowers to a single
`fmt.Sprintf` call: the literal segments become the format
string with one `%v` verb per hole, and the holes become the
trailing arguments in order.

```
"a ${e1} b ${e2} c"   →   fmt.Sprintf("a %v b %v c", e1, e2)
```

A literal `%` in a segment is doubled to `%%` so `Sprintf`
reads it verbatim; escapes in the segments are already decoded
by the parser (the format string is emitted via `strconv.Quote`
of the assembled value). The lowering pulls `fmt` into the
import set. `%v` is the universal verb — every Aril value type
(including sums / records / handles, via their Go `String()` or
struct printing) has a `%v` rendering, so interpolation is total
over any hole type. A hole may not contain a nested string
literal in v1 (lexer E0120, see `diagnostics.md`).

A module-level `let Name [: T] = Value` (ast.md §TopLevelLet) lowers
to a Go **package-level `var`**:

```
let version = 5            ⟿  var version = 5
let label: string = "aril" ⟿  var label string = "aril"
```

The annotation, when present, becomes the Go var's declared type and
seeds the initialiser's expected type (so a predeclared
`Result`/`Option` constructor gets its explicit type args — same path
as a body-level `let`). Emission is source-order; Go resolves
package-var initialisation order itself, so a constant may reference a
function or another top-level constant declared later. `var` is not a
legal top-level form (grammar.ebnf §TopLevelLet); module-scope mutable
state is a singleton class instead.

## Container types — runtime representation

```go
// package arilrt (Option/Result in runtime.go, the containers in containers.go)

type Option[T any] struct {
    Tag uint8                 // 0 = None, 1 = Some
    V   T                     // valid only when Tag == 1
}

type Result[T any, E any] struct {
    Tag uint8                 // 0 = Ok, 1 = Err
    V   T                     // valid only when Tag == 0
    E   E                     // valid only when Tag == 1
}

type Map[K comparable, V any] struct {
    m     map[K]V
    order []K                 // insertion order; appended on first .Set per K
}

type Set[T comparable] struct {
    m     map[T]struct{}
    order []T
}

type Stack[T any] struct {
    xs []T
}
```

Method bodies for these types live in the `arilrt` package
(`containers.go`). The codegen pass calls them by Go-qualified name; e.g.,
`m.set(k, v)` in Aril IR lowers to `m.Set(k, v)` in Go (note
the capital — runtime methods are exported).

**Option ⇄ JSON.** When a program uses both `Option` and an
`encoding/json` binding, the generated `Option[T]` carries
`MarshalJSON`/`UnmarshalJSON`: `None` ⇄ JSON `null`, `Some(v)` ⇄ the
JSON of `v` directly. So an Option-typed record field round-trips with
*bare* JSON (`"file": "app.log"` / `"file": null` ⇄ `Some` / `None`)
rather than exposing the internal `{Tag, V}` struct shape. The methods
are emitted only under that combined condition — a program with Option
but no json needs neither them nor the `encoding/json` import. (`Result`
has no JSON methods yet — no v1 program serialises a `Result` field;
added by analogy when one does.)

Empty-state semantics (per `builtins.md`):

- `Option.None`: `Option[T]{Tag: 0}` (no `V` — left at Go's zero
  value for `T`; codegen **never reads `V` when `Tag == 0`**, so
  the zero value is invisible to user code).
- `Some(x)`: `Option[T]{Tag: 1, V: x}`.
- `Result.Ok(v)`: `Result[T, E]{Tag: 0, V: v}` (`E` zero-valued
  and unread).
- `Result.Err(e)`: `Result[T, E]{Tag: 1, E: e}` (`V` zero-valued
  and unread).
- `Map.new()`: `&Map[K, V]{m: map[K]V{}, order: nil}`.
  Pointer receiver — methods mutate `order`.
- `Set.new()`: `&Set[T]{m: map[T]struct{}{}, order: nil}`.
- `Stack.new()`: `&Stack[T]{xs: nil}`. `Stack.pop()` returns
  `Result[T, error]` with the canonical empty-stack error
  (`arilrt.NewError("empty stack")`).

**Container brace literals** (`Set<int>{1,2}`, `Map<K,V>{}`) lower to
the same constructors, so the brace form and the `.new()` / `.from()`
form share one Go representation:

- `Set<T>{}` → `setNew[T]()`; `Set<T>{e1,…}` →
  `setFrom([]T{e1,…})` (Go infers `setFrom`'s `T` from the slice
  literal).
- `Map<K,V>{}` → `mapNew[K,V]()`. A non-empty `Map<K,V>{ k: v, … }`
  lowers to an insertion IIFE —
  `func() *Map[K,V] { m := mapNew[K,V](); m.set(k, v); …; return m }()`
  — keeping the literal a single Go expression.
- `Stack<T>{}` → `stackNew[T]()`. A `Stack` literal is always empty
  (`ast.md §BraceLit`); entries are a sema error.
- An empty `T{}` where `T` names a **class or record** is zero-value
  construction: `&T{}` (class — reference type) / `T{}` (record). A
  fieldless class is the canonical behaviour-only interface implementor
  (strategy/visitor); a field-bearing `T{}` zero-fills, like a partial
  record literal.

**Constructor type-argument stamping.** A constructor call
(`Ok`/`Err`/`Some`/`None`) constrains only the type parameter its
argument supplies — `Ok(v)` fixes `T` but leaves `E` open, `Err(e)`
the reverse, `None()` leaves `T` wholly open. Go infers a generic
function's type parameters from its *arguments only*, never from the
assignment LHS or the `return` target, so the open parameter would
make `go build` fail (`cannot infer E`). Codegen therefore stamps
**explicit** Go type arguments on the constructor from the *expected
type* in scope — the enclosing function's declared return type at a
`return`, or the annotation at a typed `let`/`var` — e.g. a `return
Ok(v)` in a `Result<int, error>`-returning function lowers to
`ResultOk[int, error](v)`, and `let x: Option<int> = None()` to
`OptionNone[int]()`. When no expected type is in scope (e.g. an
un-annotated `let`, or a constructor nested as a call argument), no
stamp is applied and Go's argument inference stands; this leaves a
nested `Ok(Ok(n))` and `wrap(Ok(x))` unstampable in v1 (the
expected type does not propagate through the inner positions).

**Tag/field invariants.** Codegen and the runtime helpers
guarantee:
- `Option.V` is read only when `Tag == 1`.
- `Result.V` is read only when `Tag == 0`.
- `Result.E` is read only when `Tag == 1`.

`MatchIR` lowering enforces this — the `case` for `Tag == n`
reads only the field associated with that tag. Pattern
desugaring in `desugaring.md` Stage 4 (`try`) and Stage 5
(`match`) preserves the invariant.

**`try` lowering (preamble specialisation).** Although
`desugaring.md` Stage 4 models `try e` as a `match` rewrite,
codegen specialises the two well-known shapes (Result / Option)
to an inline *early-return preamble* rather than a full match —
semantically equivalent, smaller output:

```
__aril_try_N := e
if __aril_try_N.Tag == <bail> {        // 1 = Err (Result), 0 = None (Option)
	return <wrapped bail of the enclosing return type>
}
// the value of `try e` is __aril_try_N.V
```

In **expression position** (`f(try e)`, `a + try e`) the preamble
cannot sit inline, so it is **hoisted** to precede the enclosing
statement; the `try` node itself lowers to `__aril_try_N.V`.

Hoisting is only applied when it preserves observable evaluation
order. Lifting a `try`'s early-return ahead of the surrounding
expression would defer (or, on bail, skip) any *side-effecting
expression evaluated before it* — so a `try` is hoisted only when
every expression preceding it in its frame is pure; otherwise the
`try` is left in place and rejected (lift it to a `let`/`var`/
`return` binding). Two adjacent tries are always safe — both move
out, in order — which is the common shape (`f(try a(), try b())`).
The walk also stops at any construct introducing a new return
frame (closure, value-position `match`/`if`/block, `scope`/`spawn`)
— a `try` there belongs to that frame — and does not descend the
right operand of `&&`/`||` (conditional evaluation; an
unconditional preamble would change short-circuit semantics).

`arilrt.NewError(msg string) error` is a thin wrapper around
`errors.New(msg)` from the Go stdlib; signature `func
NewError(msg string) error`. It exists so codegen can emit
short typed errors without import-rewriting the standard
`errors` package for every internal use.

The user-level `error(msg): error` free constructor
(`builtins.md` §error) lowers directly to `errors.New(msg)`,
pulling in Go's `errors` import on demand (it is a stdlib call,
not an `arilrt` helper, so it is the same in both runtime modes). It is recognised by the bare-`error` identifier callee
with exactly one argument — the `error(): string` interface
method takes none, and `.error()` calls are receiver-qualified,
so the form is unambiguous.

## Channel lowering

```
Channel<T>      → chan T              (bidirectional)
SendChan<T>     → chan<- T            (send-only)
RecvChan<T>     → <-chan T            (recv-only)
makeChannel<T>(cap)   → make(chan T, cap)         (cap = 0 if absent)
```

`ch.send(v)` → `ch <- v`. `ch.recv()` → `<-ch`. `ch.tryRecv()`
→ a select with a default case:

```go
func tryRecv[T any](ch <-chan T) arilrt.Option[T] {
    select {
    case v := <-ch:
        return arilrt.Option[T]{Tag: 1, V: v}
    default:
        return arilrt.Option[T]{Tag: 0}
    }
}
```

`ch.close()` → `close(ch)`. The widening of `Channel<T>` to
`SendChan<T>` / `RecvChan<T>` at argument sites is handled by
Go's own conversion rules — `chan T` is assignable to `chan<-
T` and `<-chan T` implicitly. No runtime cost.

## SelectStmt

A `select { … }` lowers to a Go `select` statement, one case per
arm (T-Select : unit):

```
case x = <-ch => B     ⟿   case x := <-ch: <lowering of B>
case <-ch => B         ⟿   case <-ch:      <lowering of B>   // drop
case ch.send(v) => B   ⟿   case ch <- v:   <lowering of B>
default => B           ⟿   default:        <lowering of B>
```

The `x :=` binding is emitted only for a named receive (dropped for
`<-ch` and for `_`). The recv channel operand reuses the `<-ch`
operator lowering above; the send case reuses `ch <- v`.

## TopContextExpr

```
TopContextExpr                          ⟿   context.Background()
```

`TopContextExpr` is the no-parent placeholder produced by
desugaring at the root `scope` call site when the source has
no explicit parent context. It lowers to a single Go
expression with no side effects.

## ScopeIR / SpawnIR

```
ScopeIR { group_name: g, ctx_name: ctx, parent: P, body: B,
          result_ty: Result<T, E> }
                                       ⟿
  (in Go, as an expression — wrapped in an immediate function:)

  func() arilrt.Result[T, E] {
    eg, ctx := arilrt.NewGroup(<lowering of P; defaults to
                           context.Background()>)
    _ = ctx                                     // ScopeRef reads ctx; the
                                                //  blank assign covers a
                                                //  scope that never does
    <lowering of B>                             // ends with a trailing
                                                //  arilrt.Result[T, E]
                                                //  value or unit-Ok
    if err := eg.Wait(); err != nil {
        return arilrt.Result[T, E]{Tag: 1, E: err.(E)}
    }
    return <the trailing-expression Ok-wrap>
  }()
```

**Group helper (no external dependency).** Generated modules carry no
`errgroup` import — so the group is the `arilrt` `Group` helper
(vendored mode) or its inline equivalent (single-file mode), emitted
conditional on a `scope` appearing. It is built from `sync` + `context`
and
replicates `errgroup.WithContext` semantics: the first spawned func
to return a non-nil error stores it (once) and cancels the derived
context; `Wait` blocks for every spawn and returns that error.
`NewGroup(parent)` returns `(*Group, context.Context)`. Like
`arilrt.Result` / the containers, its home is the `arilrt` package
(vendored mode imports it; inline single-file mode emits it into the
prelude).

`ScopeRef` (the `scope` identifier — value access to the scope's
context) lowers to the derived-context variable bound above: a
`scope.context` reference emits `ctx` (the nearest enclosing scope's
context). The binding is always emitted with a defensive `_ = ctx` so a
scope that never reads it still compiles. Outside any scope block a
`ScopeRef` is **E0601** (sema).

```
SpawnIR { parent_group: g, parent_ctx: ctx, body: B }
                                       ⟿
  g.Go(func() error {
    <lowering of B, with the body's Result<unit, E> returns
     converted to the func's `error` return:
       return Ok(_)   ⟿  return nil
       return Err(e)  ⟿  return e          // E = error, no assertion
       return <other Result expr r>
                      ⟿  if r.Tag == 1 { return r.E }; return nil>
    return nil                              // fall-through (body has no
  })                                        //  trailing return)
```

A spawn body in the corpus ends in an explicit `return Ok(())` /
`return Err(e)`, so the conversion is applied per-return; the
trailing `return nil` is emitted only when the body falls through
without a return.

**`try` inside a spawn body.** A spawn body is an implicit
`Result<unit, error>` frame (it returns `error` to the group), so a
`try e` inside it is permitted regardless of the enclosing function's
return type (sema: the try-forbidden flag is cleared for the body, so
no E0402). It lowers to the same temp + bail as a statement `try`,
except the bail returns the inner Result's **error** directly — matching
the `func() error` signature — rather than a wrapped Result:

```
let x = try e   (in a spawn body)
                                       ⟿
  tmp := <lowering of e>
  if tmp.Tag == 1 { return tmp.E }     // Err ⟿ return the error
  x := tmp.V
```

The asserted-`E` (`res.E.(error)`) is safe because **v1
restricts `scope<T, E>` to `E = error`**. Any other `E` is
rejected by sema with **E0407 `scope` error parameter must be
`error` in v1** (paired with `type-system.md`; the relaxation
to arbitrary `E` is parked until a typed-error adapter lands
in the runtime). Every example in the corpus uses
`scope<T, error>`, so v1 is unaffected. Codegen relies on this
restriction — it never sees `E != error`.

## MatchIR

A `MatchIR` lowers to a Go `switch` statement; each `BranchIR`
becomes a `case`. The shape varies by `BranchIR.tag`:

```
BranchIR { tag: VariantTag(V), payload_binds: [b_1, ..., b_n], body: E }
                                       ⟿
  case <subject>.Tag == <tag-int-for-V>:
    b_1 := <subject>.<payload-field-for-1>
    ...
    b_n := <subject>.<payload-field-for-n>
    <lowering of E>

BranchIR { tag: LiteralValue(L), payload_binds: [], body: E }
                                       ⟿
  case <subject> == <L's Go literal>:
    <lowering of E>
```

When all branches share the same head (`==` for primitives, or
all `Tag == N` for variants), codegen prefers a `switch` with
multiple `case` arms over a chain of `if`. For an
`UnreachableIR` leaf, codegen emits
`panic("unreachable: non-exhaustive match")`.

### Bind-and-ignore guard

Go rejects a local that is declared and never used, but a pattern
that binds a payload it does not consume (`Err(e) => { os.exit(1) }`)
is idiomatic in every match-bearing language. So when the arm body
holds no value reference to a payload binding `b`, codegen follows
`b := <subject>.<field>` with a blank read:

```
  case <subject>.Tag == <tag>:
    b := <subject>.<field>
    _ = b                       // only when the arm body ignores b
    <lowering of body>
```

Used-ness is decided by sema (a binding's `Symbol.Used` records whether a
value reference resolved to it, respecting shadowing), so a binding the
body *does* use emits no guard and the lowering is unchanged. The same
guard covers the index/value binders of a `for (i, x) in …` loop
(§For-loops) — a component the body ignores is blank-read.

Three bindings are exempt because the emitted Go uses them even when the
source body does not, so no guard is needed:

- a numeric range-counter (`for i in 0..n`) — the emitted `i++` clause
  uses it (codegen skips the guard for the `RangeExpr` form);
- a Map key (`for (k, v) in m`) when the value is bound — the emitted
  `v := m.At(k)` uses the key (guarded only when the value is `_`);
- a loop-invariant reference — sema does not count it toward `Used`, since
  codegen elides the invariant in the default (off) contract mode.

### Tuple value-switch — boolean decision tree

A `match` whose subject is a tuple `(s_1, ..., s_k)` cannot switch
on a single tag. It lowers to a Go `switch {}` (a boolean decision
tree): each component is captured in a temp, and each tuple-pattern
arm becomes a `case` whose guard is the conjunction of its
components' tests.

```
match (s_1, ..., s_k) { (p_1, ..., p_k) => E, ... }
                                       ⟿
  __t_1 := <s_1>;  ...;  __t_k := <s_k>
  switch {
  case <cond(p_1, __t_1)> && ... && <cond(p_k, __t_k)>:
    <binds(p_1, __t_1)> ... <binds(p_k, __t_k)>
    <lowering of E>
  ...
  }
```

A component pattern contributes a `cond` and `binds`:

- `VariantPat V(...)` → `__t.Tag == <tag-for-V>`; payload
  sub-patterns bind against `__t.<payload-field>` exactly as in the
  single-subject case.
- constructor-ident `V` (nullary) → `__t.Tag == <tag-for-V>`; no
  bind.
- literal `L` → `__t == <L's Go literal>` (`bool` uses `__t` / `!__t`);
  no bind.
- wildcard `_` → no `cond`, no bind.
- fresh ident `x` → no `cond`; binds `x := __t`.

A component ident counts as a constructor reference only when it
names a variant of *that component's own sum type* (resolution is
scoped to the component type, not the global variant set) — so a
fresh-binding ident colliding with an unrelated sum's variant binds,
and a constructor naming a different sum than the component's type is
a mismatched-constructor error.

An arm whose every component is a wildcard or fresh ident has an
empty conjunction and lowers to `default:`. Arm order is preserved,
so Go's first-match `switch` semantics coincide with Aril's. The
trailing `UnreachableIR` guard is emitted unless some arm produced a
`default:` (which already makes the Go switch terminating — a guard
after it would be unreachable code). Refining a component *inside* a
payload (`(Idle, Select(Cola))`) is not lowered in v1: payloads are
bound, not tested.

### `match` in value position

A `match` whose result is consumed (LHS of an assignment, RHS of
a `let`/`var`, argument of a call) lowers to a Go IIFE:

```
let r = match subject { p_1 => e_1, ..., p_n => e_n }
                                       ⟿
  r := func() T {
    switch subject(.Tag)? {
      case <head-1>: return e_1
      ...
      case <head-n>: return e_n
    }
    var __zero T; return __zero
  }()
```

`T` is the unified type of the arm bodies per `T-Match`. The
trailing zero-value return is unreachable when the match is
exhaustive but required by Go's reachability checker for any
switch without a `default:`. Payload-binding patterns in
value-position match aren't supported in this IIFE form — but a
match in **tail position** (the trailing expression of a
value-returning body) does support them, lowering as a
statement `switch` whose arms `return` (see §Implicit tail
return below).

**Variant-tag numbering.** For built-in sum types the tag ints
are fixed by `builtins.md`: `None = 0`, `Some = 1`, `Ok = 0`,
`Err = 1`. For user-defined sum types the tag is the
**declaration order** of the variant in the `TypeDecl` (first
variant = 0, second = 1, …). The runtime never persists
tags across runs, so re-ordering variants is a source-level
change with no runtime stability concerns.

**Recursive sum types.** A payload field whose declared type
directly names the enclosing sum (`Node(left: Tree, right: Tree)`,
or `Tree<T>` for the generic form) would make the lowered Go struct
infinitely sized — Go forbids a struct that contains itself by
value. Such a field is **pointer-ized**: the struct field becomes
`*Tree` (resp. `*Tree[T]`), the constructor stores the address of
its by-value parameter (`NodeLeft: &left`), and the match-binding
dereferences (`l := *subject.NodeLeft`) so the bound name keeps the
sum's value type. Aril sum values are immutable, so the introduced
sharing is unobservable. Only the **direct** self-reference is
detected; recursion routed through a slice / map / channel
(`[]Tree`, `Map<K, Tree>`) is already an indirection in Go and is
left as-is, while by-value recursion nested inside another generic
(`Option<Tree>`) is a v1 limitation.

## Implicit tail return

A function / method / closure body is a block, and a block's
value is its trailing expression (the block-as-expression value
rule; `type-system.md` §T-Block). When the body's declared
result is a value (return type ≠ `unit`), the trailing
expression is an **implicit return** — codegen emits it in
*tail position* rather than discarding it:

```
func f(...): R {                func f(...) R {
  <stmts>                          <stmts>
  <trailing-expr>      ⟿           <trailing-expr in tail position>
}                               }
```

Tail position **distributes** the `return` into the leaves of a
trailing `match` / `if` / block rather than wrapping the whole
body in a value-position IIFE (§match in value position):

- **plain value `e`** ⟿ `return e`;
- **`match`** ⟿ the statement `switch` (so payload-binding arms
  lower cleanly, unlike the IIFE form), each arm body emitted in
  tail position. A trailing `panic("unreachable: non-exhaustive
  match")` is emitted after a `switch` with no `default:`, since
  such a `switch` is not a terminating statement in Go even when
  the match is exhaustive (Go would otherwise report "missing
  return");
- **`if`** ⟿ the statement `if`, each branch's trailing in tail
  position; both branches are required (an else-less value `if`
  has no value on the else path);
- **block** ⟿ its statements, then its trailing in tail position;
- a **diverging** trailing (`return` / `break` / `continue` /
  `os.exit`) already terminates control and is emitted as-is,
  with no `return` wrapper.

The declared return type is in scope at every leaf, so a leaf
`Result` / `Option` constructor gets explicit Go type arguments
stamped (§"Constructor type-argument stamping"). A `unit`-result
body keeps the statement-position discard (the trailing
expression is evaluated for side effects only); a body ending in
explicit `return`s has no trailing and emits nothing extra.

A short closure whose body is a block that yields **only** via
`return` — `(a, b) => { if a > b { return a } return b }` — is
lowered the same way: the block's statements become the func
literal's body directly (the inner `return`s are the closure's
returns), not a value-position IIFE (which cannot express a
trailing-less block). The closure's Go return type comes from the
collected `return` types (`type-system.md` §T-Closure-Block).

## Implicit receiver / Field

`Field { receiver: This{type: C}, name: n }` lowers to the Go
expression `t.N` — `t` is the receiver name chosen for the
generated Go method (codegen uses `t` consistently for clarity;
not exposed in Aril), and the field is exported per §"Record /
struct field lowering".

Generic class methods carry their type parameters as Go-side
type params; the receiver is a pointer for any class with
`var`-modified fields (mutation visible across calls); a value
receiver otherwise. For v1 every class uses a pointer receiver
unconditionally — keeps the lowering uniform; the few
pure-value classes pay an unnoticeable indirection cost.

**Go-error method rewrite.** A method call `e.error()` on a value
whose sema type is the predeclared `error` builtin lowers to Go's
`e.Error()` — the PascalCase↔lowerCamel binding-name convention at
the Go boundary (a **D6** rule, cf. the D14 footnote). This is
gated on the *receiver's sema type*: a user class that
`implements error` is a nominal `Named` type, not the `error`
builtin, so its own declared `error()` method lowers unchanged to
`t.error()`. (v1 hand-codes this single boundary method; the
general exported-method rewrite arrives with the bindgen pipeline.)

## Slice methods

```
s.len()           → len(s)
s.push(e)         → append(s, e)              (returns the new slice)
s.copy()          → append(s[:0:0], s...)     (fresh-backing clone: the
                                                zero-cap reslice forces
                                                append to allocate, so the
                                                result never aliases s —
                                                expression-form, no element
                                                type named; the receiver is
                                                emitted twice, so v1 expects
                                                a side-effect-free receiver
                                                — an Ident in the corpus)
s[i]              → s[i]                       (panics on out-of-bounds,
                                                Go semantics)
s[lo:hi]          → s[lo:hi]
```

Slice index-write `s[i] = v` lowers to `s[i] = v` directly.

## Defer / panic / refEq

```
defer call(args...)   →  defer call(args...)
panic(msg)            →  panic(msg)
refEq(a, b)           →  a == b                (Go interface / pointer
                                                identity; sema has
                                                guaranteed C_a = C_b
                                                via T-RefEq)
```

`panic` always reaches Go's runtime panic mechanism — there is
no Aril-level recover (D7 / cut). Bound stdlib calls that may
panic at the Go level propagate naturally.

## For-loops

```
ForRangeIR { iter: s : []T, bind: x, body: B, indexed: false }
                                       ⟿
  for _, x := range s {                       // _ discards the index
    <lowering of B>
  }

ForRangeIR { iter: s : []T, bind: (i, x), body: B, indexed: true }
                                       ⟿
  for i, x := range s {
    <lowering of B>
  }

ForRangeIR { iter: IntRange{lo, hi, inclusive: false} }
                                       ⟿
  for i := <lowering of lo>; i < <lowering of hi>; i++ {
    <lowering of B>
  }

ForRangeIR { ... inclusive: true }
                                       ⟿
  for i := <lowering of lo>; i <= <lowering of hi>; i++ {
    <lowering of B>
  }

ForRangeIR { iter: s : string, str_runes: true }
                                       ⟿
  for _, r := range s {                       // Go's `range string` yields runes
    <lowering of B>
  }
```

```
ForMapIR { iter: m : Map<K,V>, bind: (k, v), body: B }
                                       ⟿
  for _, k := range m.Order() {               // m.Order() returns []K
                                              //  in insertion order
    v := m.Get(k).V                           // Map.Get → Option, .V is the
                                              //  value (always Some here
                                              //  because we just read a
                                              //  known key)
    <lowering of B>
  }

ForSetIR { iter: s : Set<T>, bind: x, body: B }
                                       ⟿
  for _, x := range s.Order() {               // insertion order
    <lowering of B>
  }

ForChanIR { iter: ch : RecvChan<T>, bind: x, body: B }
                                       ⟿
  for x := range ch {                         // exits on close
    <lowering of B>
  }
```

The `Order()` accessor on `Map` and `Set` is in the runtime
package; it exposes the insertion-order slice.

## Tuple destructuring

```
LetIR { pat: (p_1, ..., p_n), value: e }
                                       ⟿
  tmp := <lowering of e>           // value bound once (side-effects)
  p_1 := tmp._0                    // IdentPat component
  p_2 := tmp._1
  ...                             // `_` components bind nothing
```

`let (a, b) = e` evaluates `e` once into a fresh temp, then binds each
component positionally off the anonymous-struct tuple representation
(`tmp._0`, `tmp._1`, …), recursing for a nested tuple component. A
binding whose every component is `_` discards the value (`_ = e`)
rather than leaving an unused temp. Refutable / arity-mismatched
patterns are rejected in sema (T-Let-Destructure), so the lowering
only ever sees irrefutable name/`_`/tuple patterns.

## While-loops

```
WhileIR { cond: C, body: B }   ⟿   for <lowering of C> { <lowering of B> }

WhileIR { cond: true, body: B } ⟿   for { <lowering of B> }
```

`while true` lowers to Go's condition-less `for { … }`, **not**
`for true { … }`. Only the condition-less form is a *terminating
statement* in Go: a `while true` whose sole exits are `return`s in
the body (a common shape for `match`-driven loops) would otherwise
draw a spurious "missing return" after the loop. The literal `true`
is recognised through redundant parentheses.

### LabeledBreak

An Aril `break` always targets its nearest enclosing loop. A `match`
lowers to a Go `switch` (§MatchIR) and a `select` to a Go `select`,
**both of which capture a bare Go `break`**. So a `break` written
inside a `match` arm (or `select` case) nested in a loop would break
the switch, not the loop — silently turning the loop infinite. When a
loop body contains such a break, the loop is emitted with a generated
Go label and those breaks lower to `break <label>`:

```
while true { match i { 3 => break, _ => { i = i + 1 } } }
  ⟿
_arilLoop1:
	for {
		switch i {
		case 3:
			break _arilLoop1
		default:
			i = i + 1
		}
	}
```

The label is emitted **only** when needed: a `break` directly in the
loop body (no intervening switch/select) lowers to a bare `break`, and
a loop with no captured break gets no label (so existing lowerings stay
byte-identical). `continue` needs no treatment — Go's `switch`/`select`
do not capture it, so a bare `continue` already targets the loop.

## Contracts — loop invariants (RFC-0006)

A labelled loop carrying a `contract … { loop <label> { invariant P } }`
section lowers its invariants to a per-iteration check, emitted at the **end of
the loop body** (so it runs after every iteration). Under `--contracts=panic`:

```
for … { <body> }   with invariant P
  ⟿
for … {
    <lowering of body>
    //line <src>:<P-line>
    if !(<lowering of P>) {
        panic("aril: contract: loop invariant violated (loop <label>)")
    }
}
```

The `//line` directive at the check maps the panic back to the predicate's
`.aril` source, so blame reads in Aril coordinates (D10) with no runtime
support. Under `--contracts=off` (the default during the build-out) **nothing
is emitted** — a contracted program lowers byte-identically to the same program
without the contract (golden-fixture and `build_ok`-ratchet safe).

### requires / ensures / entry (RFC-0006)

A function contract lowers with a **Go named return value** so the `ensures`
post-check sees the returned value at every return path without rewriting
returns:

```
func f(p…) RetType { <body> }   with entry { let n = e }, requires R, ensures S
  ⟿
func f(p…) (_arilRet RetType) {
    _arilEntry_n := <e>; _ = _arilEntry_n     // entry snapshots (function entry)
    <guarded check of R>                       // requires (function entry)
    defer func() {
        if r := recover(); r != nil { panic(r) }   // skip on a panic-in-progress
        <guarded check of S>                        // ensures (normal return)
    }()
    <body>                                      // each `return X` sets _arilRet
}
```

In a predicate, `result` lowers to `_arilRet` and an `entry`-binding name `n`
to its temp `_arilEntry_n`. A **named return** is emitted only when the contract
has `ensures` (and a non-unit return type); `requires`-only needs no named
return.

Each check is wrapped in a **re-entrancy guard** so a predicate that calls the
contracted (or a mutually-contracted) function does not recurse without bound
(`ensures setEq(result, union(b, a))` inside `union`):

```
if !_arilInContract {
    _arilInContract = true
    _arilPass := (<pred>)        // nested calls see the flag set → skip their checks
    _arilInContract = false
    if !_arilPass { panic("aril: contract: <kind> violated (<fn>)") }
}
```

`_arilInContract` is a package-level flag emitted once when any function
contract is lowered. Two v1 limitations, both superseded by the future
`arilrt` contract layer with a goroutine-local save/restore: it is not
goroutine-safe (single-threaded contract checking), and the `= false` reset is
not panic-safe — a predicate that itself panics leaves the flag set. Both are
inert in v1: sequential Aril has no source-reachable panic recovery, so a
predicate panic aborts the process (no continuation can observe a stuck flag).
Under `off`, none of this is emitted (byte-identical).
`warn` / `stats` modes and the `arilrt` violation-rendering layer (richer blame,
mode tally) are not lowered yet.

### Type invariants — construction + method exit (RFC-0006)

A type carrying a `contract <Type> { invariant P }` is checked at two
checkpoints. The predicate resolves in the type's field scope (bare field
names), and the guarded check is the same `_arilInContract` re-entrancy form
as requires/ensures, with `<kind>` = `invariant` and `<fn>` = the type name;
the `//line` at the predicate maps blame to the invariant's `.aril` source
(D10). Under `off`, nothing is emitted (byte-identical).

**Construction.** Every brace literal of an invariant-bearing type lowers
inside an IIFE that validates the freshly-built value before it is used — the
**only** checkpoint for a record (no methods), and a complement to the
method-exit check for a class (catching an object built but never
method-called). The predicate's field names lower against the construction
temp `_arilNew`:

```
T{ … }   with invariant P            (T value type; a class is *T)
  ⟿
func() T {
    _arilNew := T{ … }
    <guarded check of P, fields → _arilNew.<field>>
    return _arilNew
}()
```

**Method exit.** A **class** additionally checks each invariant at every
non-static method exit — the mutation boundary, where a transiently-broken
invariant (e.g. `size <= capacity` while an insert outruns the eviction that
restores it) must hold again. Each non-static method lowers with a `defer` that
runs the guarded check on every return path; the predicate's field names lower
through the implicit receiver to `t.<field>`, so the check reads the
post-mutation state:

```
class C { … }   with invariant P            (m is a non-static method)
func (t *C) m(…) R { <body> }
  ⟿
func (t *C) m(…) R {
    defer func() {
        if r := recover(); r != nil { panic(r) }   // skip on a panic-in-progress
        <guarded check of P, fields → t.<field>>     // invariant (method exit)
    }()
    <body>
}
```

A **static** method has no receiver and gets no method-exit check (only the
construction IIFE around its returned literal). Rejecting a direct external
field write that could break an invariant between checkpoints (E1106) is a
later slice.

## Channel contracts — the per-channel monitor (RFC-0007)

A channel trace contract lowers to a per-channel **monitor** in `arilrt`
(`arilrt/contract.go`; the inline-mode prelude mirrors it byte-for-byte). v1
enforces the definitive **local** subset — `forbid send after close` (E1203),
double close (E1202), and the `drains-before-…` completion check (E1207) —
keyed by the channel **value**, so a registered channel is monitored wherever
it flows (including across `spawn`/`scope`); the state is mutex-guarded.
Everything is gated on `--contracts=panic`; under `off` **nothing is emitted**
(byte-identical lowering).

**Registration** binds by NAME at the channel's creation site (an `Ident` whose
name is a contracted subject, Info.ChannelContracts) over a **bidirectional**
`Channel` value — the creator/owner frame where the channel is `chan T`.
`RegisterChan` records the channel under **every directional view** (`chan T`,
`chan<- T`, `<-chan T`) sharing one state, so a send/close from a callee frame
that received the channel as a directional `SendChan`/`RecvChan` parameter still
finds the monitor (the views box to distinct dynamic types, so a single-view key
would miss). The `forbidSend` registration flag carries whether `forbid send
after close` (E1203) applies to the subject.

**Routing** a `.send` / `.close` through the monitor:

- a **named bidirectional** receiver matches its own subject clauses precisely
  (byte-identical to the pre-directional lowering — uncontracted bidi channels
  stay raw);
- a **directional `SendChan`** receiver has lost the source subject name across
  the function boundary, so it routes whenever the program carries *any* relevant
  contract (`forbid send after close` for sends; any enforced subject for
  closes); the runtime monitor no-ops an unregistered channel.

Bidi **aliasing** (a contracted channel rebound to another bidirectional name)
is the one remaining unmonitored path — a follow-up.

```
let ch = makeChannel<T>(cap)     channel ch { forbid send after close; drains-before-scope-exit }
  ⟿
ch := make(chan T, cap)
arilrt.RegisterChan(ch, "ch", true)                 // register (all views); forbidSend flag
defer arilrt.ChanCheckDrained(ch, "<file:line:col>") // boundary drain check (drains subjects)

ch.send(v)   ⟿   arilrt.ChanSend(ch, v, "<loc>")     // asserts open before send (E1203)
ch.close()   ⟿   arilrt.ChanClose(ch, "<loc>")       // double-close (E1202); records closed
out.send(v)  ⟿   arilrt.ChanSend(out, v, "<loc>")    // directional callee send — cross-function (E1203)
```

`ChanCheckDrained` runs as a `defer` placed at the channel's creation site, so it
fires at the enclosing Go frame's return — the `scope` IIFE for a channel created
inside a `scope`, the function for one created in the function body. **v1 caveat:**
`drains-before-scope-exit` and `drains-before-return` therefore **collapse** to
this single creation-frame boundary — the runtime does not yet distinguish them
(a channel created inside a `scope` with `drains-before-return` is still checked
at scope exit). Pinning each clause to its own boundary is a follow-up. A
violation `panic`s with the code, the `.aril` `loc`, and the subject name (D10). The
`closed` flag set by `ChanClose` is what the send (E1203) and drain (E1207)
checks read; `drains` v1 verifies *closed*, not *closed-and-empty* (the latter
needs in-flight accounting — a follow-up). Capacity (E1206), `closed-by` (E1201),
recv-after-close (E1204), and the cross-channel trace kinds (ordering, coverage,
liveness, fairness) are recognized + well-formedness-checked (E1210) but their
runtime monitor is a follow-up.

## Generics

Aril generics lower to Go generics one-to-one:

```
class Box<T> { var v: T; static new(v: T): Box<T> { ... } }
                                       ⟿
type Box[T any] struct { v T }
func (b *Box[T]) ... { ... }
func boxNew[T any](v T) *Box[T] { return &Box[T]{v: v} }
```

Static methods lower to package-level functions named
`<class>` + capitalised method name (`boxNew`, `mapFrom`, …),
preserving the lower-case visibility convention for v1.

A **constraint bound** lowers to the matching Go constraint: an
unconstrained parameter is `any`, `<T: Ordered>` is `[T cmp.Ordered]`
(and the program gains `import "cmp"`), `<T: Comparable>` is
`[T comparable]` (a Go built-in, no import). So `func isSorted<T:
Ordered>(xs: []T): bool` ⟿ `func isSorted[T cmp.Ordered](xs []T)
bool`, and `xs[i] < xs[i-1]` is a legal Go comparison because
`cmp.Ordered` admits `<`.

A **generic class brace literal** instantiates the Go type
directly — `Box<int>{ v: 42 }` ⟿ `&Box[int]{v: 42}`. Go cannot
infer struct type parameters from a composite literal, so the
type-args are emitted explicitly; a generic class brace literal
without type-args is a codegen error. **Generic record-type
declarations** lower the same way: `type Pair<A, B> = {…}` ⟿
`type Pair[A any, B any] struct {…}`, and `Pair<int, string>{…}` ⟿
`Pair[int, string]{…}`.

**Generic sum-type declarations** (`type Tree<T> = | Leaf | Node(…)`)
carry the type params onto the tagged struct and every constructor:
`type Tree[T any] struct {…}`, `func TreeNode[T any](…) Tree[T]`. A
**nullary** variant of a generic sum cannot be a package-level `var`
(the value would need a type argument), so it becomes a parameterless
generic constructor — `func TreeLeaf[T any]() Tree[T]` — the same
shape as `OptionNone`. Go infers the type args of a *payload*
constructor call from its value arguments, but a nullary constructor
call has none, so codegen stamps explicit type args at the use site:
from the expected type in a return / typed-binding position
(`return Leaf` ⟿ `TreeLeaf[T]()`), or from the inferred instantiation
of the enclosing payload-constructor call when nested as an argument
(`Node(1, Leaf, Leaf)` ⟿ `TreeNode(1, TreeLeaf[int](), TreeLeaf[int]())`).
The instantiation is read off a value argument whose field type is a
bare type-parameter (`value: T`). A v1 limitation: if no field pins the
parameter directly (`Node(left: Tree<T>, right: Tree<T>)` with a nested
nullary `Node(Leaf, Leaf)`), the type-arg cannot be inferred at the
nullary use site and the construct does not yet lower — proper inference
for that shape is deferred to a sema generic-instantiation pass.

Type parameters lower with constraint `any` **by default**, with
one v1 exception: **constraint propagation from container key
positions**. If a generic type parameter `K` of a user decl
flows into a `Map<K, _>` key or `Set<K>` element position
(transitively, in any field or signature reachable from the
decl), codegen lowers `K` with constraint `comparable` instead
of `any`. This matches the runtime's `Map[K comparable, V any]`
and `Set[T comparable]` requirement; without it, `class
Indexer<K, V> { var m: Map<K, V> ... }` would fail Go's
type-check.

Algorithm sketch (run before lowering each user decl):

```
collect_kvars(decl):
  let kvars = ∅
  for each type expr T mentioned in decl's fields / sigs:
    if T is `Map<X, _>` or `Set<X>` and X is a type parameter:
      add X to kvars
  return kvars

constraint(α) =
  if α ∈ collect_kvars(decl): "comparable"
  else: "any"
```

For non-container constraints (e.g., `Ord`, `Stringer`), D11
parks the surface; v1 has no other constraints, so `any` /
`comparable` is the complete v1 lowering set.

For function calls that fully specify type arguments
explicitly, codegen emits the explicit form `f[T1, T2](...)`;
when type-arg inference held (per `type-system.md` unify), the
inferred substitution is used and codegen emits the explicit
form anyway (Go infers from arguments separately; the explicit
form is always safe).

## Bindings — Go stdlib

For each imported Go package (`fmt`, `os`, `strings`, ...),
the binding generator emits an `bindings/<pkg>.go` file that
re-exports the package's public API with Aril-shaped
signatures. The transformation rules:

- A Go function `func F(a A, b B) (R, error)` becomes a Aril
  function returning `Result<R, error>` — the runtime helper
  is a one-line adapter that constructs the `Result`.
- A Go function `func F(...) error` (single `error` return,
  no `R`) becomes Aril `Result<unit, error>`.
- A Go function `func F(...) R` (no error) becomes Aril
  `R`-returning.
- A Go function `func F(...) (R, bool)` (comma-ok shape)
  becomes Aril `Option<R>`.
- A Go function `func F(...)` (no return) becomes Aril
  `unit`-returning.
- Go types pass through unchanged where possible. Go-only
  receiver methods are re-exposed as Aril methods on the same
  type.
- Variadic Go parameters (`...T`) become Aril `...T`.

The full binding-surface spec is in
`../docs/binding-surface.md`; this lowering chapter only
concerns the **codegen pass that consumes** those bindings.

## ForeignCall — `extern` bindings (Go FFI)

The `extern` surface (`ffi.md`) lowers at each **use** site; the
declarations themselves emit no Go (they are signature metadata).

- **Opaque handle type.** `extern type T @go("pkg")` lowers, wherever
  `T` appears as a type, to the Go pointer type `*<ref>.Sym`, where
  `<ref>` is the import-path base name (`os/exec` → `exec`) and `Sym`
  is the `@go` symbol (default = exported Aril name). This is the
  `*regexp.Regexp` / `*exec.Cmd` shape Go libraries are used through.
- **Function call.** `f(ā)` for an `extern func f … @go("pkg.Sym")`
  lowers to `<ref>.Sym(ā)`.
- **Method call.** `r.m(ā)` on a handle `r : T` lowers to
  `r.GoName(ā)`, `GoName` from the member's `@go` (default = exported
  Aril name). Field access `r.x` lowers to `r.GoField`; a `var` field
  is assignable (`r.GoField = v`).
- **Boundary lift.** A binding whose curated return is `Result<U,
  error>` wraps its Go referent at the boundary, keyed on `U`:
  - `U ≠ unit` — the Go referent returns `(U, error)`; wrap in the
    shared `arilResultOf` helper — `arilResultOf(<ref>.Sym(ā))` —
    identical to the stdlib `resultWrap` shape above.
  - `U = unit` — the Go referent returns a **bare `error`** (no value,
    e.g. `os.Chdir`, `os.WriteFile`, `(*exec.Cmd).Run`); wrap in the
    `arilResultUnit` helper — `arilResultUnit(<ref>.Sym(ā))` — which
    folds the lone `error` into `Result<unit, error>` (`unit` →
    Go `struct{}`).

  (Comma-ok `(U, bool) → Option<U>` is a generator-side lift; its
  codegen wrapper is a later slice.)

  This boundary-lift is not extern-only: a **bound stdlib value-handle**
  method (D37 — the `internal/binding` handle table, e.g. `net.Conn`'s
  `read`/`write`/`close`) whose curated return is `Result<U, error>`
  lowers through the identical `arilResultOf` / `arilResultUnit` helper,
  keyed on `U` the same way. `net.Conn.read` (`(int, error)`) →
  `arilResultOf(conn.Read(p))`; `net.Conn.close` (bare `error`) →
  `arilResultUnit(conn.Close())`. A handle method whose return is *not*
  `Result<…>` (`regexp.Regexp.matchString → bool`) emits the bare Go call.
- **Imports.** The Go import path each used binding names via `@go` is
  added to the import block directly (it comes from `@go`, not the
  `.aril` imports). References use the path's base name; a Go package is
  imported only when one of its bindings is actually emitted.

The emitted call is **re-checked by the Go type checker** against the
real package — a binding that has drifted from its referent fails the
Go build (the "verify, don't trust" property; `ffi.md`).

The current lowering maps an opaque handle to a **pointer** Go type
(the 90 % case). Value-typed Go handles (e.g. `time.Time`) and import
aliasing for colliding base names are later slices.

## Source maps (`//line` directives)

Every emitted Go statement that originates from a `.aril` source
position carries a `//line file.aril:NN` directive immediately
above it. The runtime's panic stack traces, `go test -run`
failures, and `go vet` diagnostics will then point at the
original `.aril` coordinates — required for D10.

Conservative rule: `//line` is emitted at the *outermost* Go
statement boundary for each source construct. Fine-grained
sub-expression mapping is **not** v1; the directive form is
canonical `//line file.aril:NN:1` (line = source span's start
line, column = 1 unconditionally — Go accepts this form per
[Go spec — Source file organisation](https://go.dev/ref/spec#Source_file_organization)).

## Output formatting

Codegen emits Go that is **gofmt-stable**: piping the output
through `gofmt -s` returns it unchanged. The reason is
`test-contract.md` §`--- GO ---` — fixtures store the
post-`gofmt -s` form, so codegen and fixture must agree
byte-for-byte. The contract is therefore stronger than "go
build accepts it": **the emitted source must round-trip
through `gofmt -s` to itself**.

In practice codegen emits canonical formatting (one statement
per line, tab indent, single trailing newline, alphabetised
import groups standard / third-party / project) and runs the
buffer through `gofmt -s` as the last step before writing the
file. Production builds may skip the explicit `gofmt -s` call
if the canonicaliser already produces gofmt-stable output;
fixture comparison always re-runs `gofmt -s` to guard against
drift.

This is the *only* hand-readability concession in lowering — it
exists to keep fixtures deterministic, not because anyone is
supposed to read generated Go (D1).

## Errors — quick index

Codegen runs after all sema and desugaring checks. The only
diagnostics it raises are internal-consistency failures:

- **E0801** Internal: encountered `TryExpr` / `MatchExpr` /
  `ShortClosure` / un-typed `VariantExpr` in the IR (one of
  the desugaring stages was skipped).
- **E0802** Internal: encountered `Never`-typed value in a
  position requiring a concrete Go type (sema should have
  caught divergence-flow earlier).
- **E0803** Internal: type-arg substitution failed
  (well-formedness was violated).

Each E08xx is a bug-class — they should never reach the user
under correct sema+desugar; the compiler reports them with the
internal-error formatting.
