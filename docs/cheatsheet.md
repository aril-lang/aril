# Aril in one page — coming from TypeScript, Go, or Rust

Aril has TypeScript-style *syntax* with an ML-family *type system* (sum types,
exhaustive `match`, `Option`/`Result`), and compiles to Go. If you know TS, most
of this reads at sight — this page is about the **differences**, in the order
you meet them. Rows marked ⚠ **look like a familiar idiom but mean something
else** — read those first.

> **This page teaches the language, not the library.** *Which* modules exist and
> their exact API spelling is reference material — look it up, don't guess:
> **[`stdlib-bindings-status.md`](stdlib-bindings-status.md)** is the index of
> everything bound today (with method sets); **[`binding-surface.md`](binding-surface.md)**
> is the full target spelling. When a binding is missing, drop to Go (§Escape
> hatch). The full surface is **[`language-spec.md`](language-spec.md)**.

## 0. A complete program

An Aril program `import`s the stdlib modules it calls and defines `main` as the
entry point. ⚠ `func main` takes **no arguments and no return type** — write
`func main()`, *not* `func main(): unit`.

```aril
import fmt            // import each stdlib module you use (fmt, strings, strconv, sort, …)

func main() {         // entry point — no arguments, no `: T` return
  fmt.println("hello")
}
```

The snippets below drop this `import` / `func main` wrapper for brevity — but a
runnable program always needs both: an `import` line per module it calls, and a
`func main() { … }`.

## 1. Bindings and values

```aril
let x = 41          // immutable (like TS const / Rust let)
var y = x + 1       // mutable (like TS let / Rust let mut)
const K = 100       // just an alias for `let` — not a separate category
```

- `let` is immutable, `var` is mutable. There is **no `null`, `nil`, or
  `undefined`** — absence is `Option<T>` (§5). ⚠ `null`/`nil`/`undefined` are
  ordinary identifiers, not keywords: they will *not* do what you expect.
- **No top-level `var`.** Module-level mutable state lives in a `class` instance.

## 2. Types you write

| You mean | Aril |
|---|---|
| `number` | `int` (also `int8..64`, `uint..`, `float32/64`, `byte`, `rune`) |
| `string` | `string` (UTF-8; iterate for `rune`s, index for bytes) |
| `boolean` | `bool` |
| `void` | `unit` (its sole value is `()`) |
| a record / object type | `type User = { id: string, name: int }` — a named value type; construct by name (`User{…}`) |
| a tuple | `type Coord = (int, int)` |
| a union / enum | a **sum type** (§4) |
| `T[]` | `[]T` |
| `Map<K,V>` / `Set<T>` | `Map<K, V>` / `Set<T>` (built-in generics; also `Stack<T>`) |

Generics: `<T>`, with only two constraints — `<K: Comparable>` and
`<T: Ordered>` (no user-defined constraints in v1). ⚠ There is **no `enum` or
`struct` keyword** — use `type … = | …` and `type … = { … }` / `class`.

## 3. Functions

```aril
func add(a: int, b: int): int { return a + b }   // top-level: `func`, return `: T`
let inc = (n) => n + 1                            // closure (arrow); or func(n:int):int {…}
```

⚠ `fn`, `function`, `async`, `await`, `yield` are **not keywords** — top-level
functions use `func`; **there is no `async`/`await`** (concurrency is uncolored,
§9). String interpolation is `"…${expr}…"` (each hole → one `fmt.Sprintf %v`; no
nested string literal inside a hole).

## 4. Modeling data — sum types + `match`

This is the payoff over TS/Go. A discriminated union is a `type` with variants;
`match` is **exhaustive** (the compiler rejects a missing case, E0303):

```aril
type Tree<T> = | Leaf | Node(value: T, left: Tree<T>, right: Tree<T>)

func size<T>(t: Tree<T>): int {
  match t {
    Leaf          => 0,
    Node(v, l, r) => 1 + size(l) + size(r),
  }
}
```

Arms are `Pattern => body` (comma-separated); bodies are expressions or `{…}`
blocks. Patterns: `_` wildcard, literals, `Some(x)`/`Ok(v)` variant binds,
tuples, records, and alternatives `Up | Down =>`. Guards are an `if` **inside**
the arm body (no arm-level `if` syntax). ⚠ **No `switch`, no fallthrough.**

## 5. No null, no exceptions — `Option` and `Result`

```aril
type Option<T> = | None | Some(value: T)
type Result<T, E> = | Ok(value: T) | Err(err: E)   // Result<T> defaults E = error
```

- Absence → `Option<T>` (`Some(x)` / `None`). Failure → `Result<T, E>`.
- Consume by `match`, or the total methods `isSome`/`isNone`/`unwrapOr`
  (Option) and `isOk`/`isErr`/`unwrapOr`/`mapErr` (Result). ⚠ **No `.map`, no
  panicking `.unwrap`** in v1 — use `match` or `try`.

**Error propagation** — three tools (canonical example: `error_handling`):

```aril
let n = try strconv.atoi(s)                        // `try` = Rust `?`: unwrap Ok, else return Err
let n = try strconv.atoi(s).mapErr((e) => MyErr{…})// bridge a different error type, then propagate
let n = strconv.atoi(s) catch e { return 0 }       // no Result to return into → handler MUST diverge
```

⚠ `try` is a **prefix operator** (not a JS `try {}` block); `catch` is a
**postfix** on a `Result` whose handler must `return`/`os.exit`/`panic` — the
*opposite* of a JS `catch` that falls through. There is **no `throw`**.

## 6. Control flow

```aril
if x > 0 { … } else { … }          // also an expression: `let s = if c { a } else { b }` (both arms)
for v in items { … }               // for-in only; also `for (i,v) in xs`, `for (k,v) in m`, `for c in str`
for i in 0..n { … }                // half-open;  `1..=n` inclusive. Ranges are for-headers only
while cond { … }                   // no C-style for(;;), no do/while
x += 1                             // compound assignment is supported sugar
```

`return`/`break`/`continue` and `panic(msg)`/`os.exit(code)` never return (they
fit any typed position). `defer call` runs at **function** exit (LIFO), like Go.

## 7. Collections — the sharp edges

- Slice `xs.push(e)` **returns a new slice** (does not mutate): write
  `xs = xs.push(e)`; discarding the result is an error (E0215). ⚠ (`Stack.push`
  *does* mutate — the asymmetry is deliberate.)
- `m[k]` on a `Map` returns the **zero value** on a miss (Go semantics); the
  safe form is `m.get(k): Option<V>`.
- `==` compares tuples/records **field-wise**, but ⚠ **not class instances**
  (E0401 — use `refEq(a, b)`). ⚠ Records are **nominal named types** in v1: two
  same-shape records (`A`, `B`) are *not* interchangeable (E0201), unlike TS.
- `string → int` is **not** a cast (`int(s)` is E0205) — use `strconv.atoi`
  (`Result`) / `strconv.itoa`.

## 8. Classes (state + behavior + interfaces)

```aril
class Counter implements Greeter {
  let name: string
  var n: int
  static new(name: string): Counter { return Counter{ name: name, n: 0 } }
  bump() { n = n + 1 }                 // methods omit `func`; fields are bare (implicit receiver)
  greet(): string { return "${name}: ${n}" }
}
```

⚠ Interfaces are **nominal** — a class must say `implements I` (no Go-style
structural satisfaction). `this` is only for shadowed access; otherwise fields
and methods are referenced bare. Construct with a brace literal
`Counter{ name: "a", n: 0 }`; call statics as `Counter.new("a")`.

## 9. Concurrency — uncolored, structured

No `async`/`await`, no bare `go`. A `scope` runs `spawn`s concurrently and
**joins before it returns**:

```aril
try scope<unit, error> {             // a scope is an expression: Result<T, E>
  for w in 1..=n {
    spawn { worker(w, jobs, out); return Ok(()) }   // every spawn returns Ok(())
  }
}
```

Channels are **method-based**: `makeChannel<T>(cap)`, `ch.send(v)`, `ch.recv()`,
`ch.close()`, `for v in ch { … }` (ends on close). ⚠ Go's `ch <- v` / `<-ch`
don't exist except `<-ch` **inside a `select` case**.

## 10. Contracts (an Aril differentiator)

Executable spec written *beside* the code, enforced with `--contracts=panic`,
elided otherwise. A good contract **fires on a broken impl**:

```aril
contract add { requires b >= 0  ensures result >= a }   // requires/ensures; `result` = return
contract LRU { invariant size <= capacity }             // class invariant, checked at every method exit
channel out { forbid send after close }                 // channel trace contract → E1203
```

## 11. When a binding is missing — the Go escape hatch

Aril binds (does not port) the Go stdlib. If what you need isn't in
[`stdlib-bindings-status.md`](stdlib-bindings-status.md), drop to Go with an
`extern` declaration:

```aril
extern type Cmd @go("os/exec")
extern func command(name: string, args: []string): Cmd @go("os/exec.Command")
```

Generate a starting binding from any Go package with `aril import <path>`. And
to see exactly what your Aril lowered to, **`aril emit <file.aril>`** prints the
Go (`-no-line` for a clean read) — Go is the IR, and you can always read it.

## Trap table — looks-like-TS, means-something-else

| You'd expect (TS/Go/Rust) | Aril reality |
|---|---|
| `null` / `undefined` | none — `Option<T>` (`Some`/`None`) |
| `try { } catch { }` block | `try` is prefix (`?`); `catch` is postfix + **must diverge** |
| `throw` / exceptions | none — `Result<T, E>` |
| `arr.push(x)` mutates | slice `push` **returns a new slice**; `xs = xs.push(x)` |
| `map[k]` → undefined on miss | zero value; use `m.get(k): Option<V>` |
| `a == b` on objects | class instances: `refEq(a,b)` (E0401); records/tuples compare field-wise |
| same-shape records interchangeable | nominal named types — `A` ≠ `B` (E0201) |
| `Number(s)` / `int(s)` | `strconv.atoi(s): Result<int,error>` (E0205 on a cast) |
| `enum` / `struct` keyword | `type X = | A | B(…)` / `type X = { … }` / `class` |
| `async` / `await` / `go f()` | uncolored: `scope { spawn { … } }` |
| structural interfaces (Go) | nominal: `class C implements I` |
| `fn` / `function` | `func`; methods omit it |
