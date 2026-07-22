# Aril gotchas — where a familiar idiom means something else

Aril reads like TypeScript, so a TS/Go/Rust developer arrives with strong
priors. Most transfer. This page collects the ones that **don't** — the places
where valid, compiling Aril behaves differently from what your prior predicts.
None of these is a bug: each is a deliberate, defensible choice (usually
inherited from Go's runtime, sometimes safer than Go). But a surprise that
compiles and runs is exactly the kind that bites silently, so they are gathered
here in one place.

For the fast per-topic diff, read [`cheatsheet.md`](cheatsheet.md) first — its
**Trap table** is the one-line version of the highest-frequency rows below. This
page is the *why* behind them plus the long tail.

Legend: **≈Go/Rust** = Aril matches Go (and usually Rust); the surprise is only
versus a TypeScript prior. **Aril-specific** = the behaviour is particular to
Aril's surface.

---

## Numbers

### Integer division truncates; `5 / 2` is `2` — not `2.5` **≈Go/Rust**

`int` division discards the remainder. This is the single most common
TypeScript-developer arithmetic surprise, because TS's one `number` type makes
`5 / 2` be `2.5`.

```aril
fmt.println(5 / 2)        // 2   (not 2.5)
fmt.println(int(2.9))     // 2   (int(...) truncates, does not round)
```

For a real quotient, operate on `float64`: `5.0 / 2.0` → `2.5`. To *round* a
float instead of truncating, use `math.round` (which rounds half away from
zero — see [Formatting and bindings](#formatting-and-bindings) below).

### Fixed-width integers wrap silently on overflow **≈Go/Rust**

There is no auto-promotion to a bignum. Overflow, underflow, and narrowing all
wrap modulo the type width, with no panic:

```aril
fmt.println(int8(127) + 1)   // -128   (wraps)
fmt.println(int8(300))       // 44     (narrowing wraps)
// uint 0 - 1  →  18446744073709551615
```

### `0.1 + 0.2` prints `0.3` on literals but `0.30000000000000004` on variables **Aril-specific**

The canonical "floats are imprecise" demo has *two* answers in Aril, depending
on whether the operands are literals or variables:

```aril
fmt.println(0.1 + 0.2)              // 0.3
let a = 0.1
let b = 0.2
fmt.println(a + b)                  // 0.30000000000000004
```

Literal operands are const-folded at compile time using arbitrary-precision
arithmetic (the exact nearest `float64` to true 0.3, which *prints* as `0.3`);
variables do ordinary IEEE-754 at runtime. The runtime answer
(`…04`) is the true one — a spot-check with literals gives false confidence. If
you want to reproduce a floating-point subtlety, bind the operands to variables.

### Float printing uses Go's `%g`: `1000000.0` → `1e+06`, `1.0` → `1` **≈Go**

`fmt.println` / `${}` render a float with Go's default verb, which switches to
scientific notation for large/small magnitudes and drops a trailing `.0`:

```aril
fmt.println(1000000.0)     // 1e+06
fmt.println(1234567.5)     // 1.2345675e+06
fmt.println(1.0)           // 1        (indistinguishable from an int)
```

TS and Rust print plain decimal well past this range. There is no built-in
fixed-notation formatter yet; drop to Go's `strconv.FormatFloat` via an `extern`
if you need one.

### Integer `10 / 0` panics; float `1.0 / 0.0` is `+Inf` **≈Go/Rust**

A TS developer expects `Infinity` from both. Aril follows Go: *integer*
divide-by-zero is a runtime panic; *float* divide-by-zero yields the IEEE
infinities.

```aril
let d = 0
fmt.println(10 / d)        // panic: runtime error: integer divide by zero
fmt.println(1.0 / 0.0)     // +Inf   (no panic)
```

A *constant* `10 / 0` is caught at compile time instead. The runtime panic's
coordinate maps back to your `.aril` source, but the message and goroutine trace
are still Go's — pipe them through `aril explain` (see
[`aril-explain.md`](aril-explain.md)) to reframe them.

---

## Strings

### `.len()` is bytes; `for c in s` yields runes that print as integers **≈Go, partly**

A `string` is UTF-8 bytes by storage but iterates as runes (Unicode
codepoints). Two consequences bite:

```aril
fmt.println("héllo".len())     // 6   (bytes; TS UTF-16 .length is 5)
for c in "héllo" { fmt.println(c) }
// 104 233 108 108 111  — each c is a `rune`, printing as its codepoint integer
```

Iterating gives you **5** runes (correct character count), but each `c` is a
numeric `rune`. To print it *as a character*, convert: `string(c)` → `h é l l o`.
Byte length and rune iteration are both intentional (they match Go and the
underlying memory); the trap is that a bare `fmt.println(c)` in the loop prints
numbers.

---

## Values and collections

### Record copy is *shallow*: reference-typed fields stay aliased **Aril-specific**

Records are value types, so `var b = a` copies the scalar fields independently.
But a `List`/`Map`/`Set` field holds a **reference**, and the copy shares it —
both records observe each other's mutations through that field:

```aril
type Bag = { tag: int, items: List<int> }
let a = Bag{ tag: 1, items: List<int>{} }
var b = a
b.tag = 99          // independent — a.tag stays 1
b.items.push(7)     // ALIASED — a.items is now [7] too
// a: tag 1, items [7]     b: tag 99, items [7]
```

"Records are value types" is true for the scalar fields; it is *not* a deep-copy
guarantee. If you need an independent list, copy it explicitly.

### `[]T` allows `s[i] = v` (and aliases its backing array) — even through `let` **≈Go**

The cheatsheet calls `[]T` a "value view — pure accessors only, no `.push`". That
is about *growth*: you cannot append. But **element assignment is legal** and
mutates the shared backing array, through a `let` binding and through a
sub-slice that aliases its parent:

```aril
let s = List<int>{1, 2, 3}.toSlice()
s[0] = 99                 // legal — mutates in place (no error, even via `let`)
```

Two asymmetries to keep in mind: index-*assignment* on `[]T` is not "pure" the
way `.copy()`/`.len()`/`s[i]` reads are; and `[]T` allows `s[i] = v` while
`List` **forbids** `l[i] = v` (you must call `l.set(i, v)`). When you want a
detached copy, use `s.copy()`.

### Bare `m[k]` returns the zero value on a miss **≈Go**

`m["absent"]` gives `0` (or `""`, or the field-zeroed struct) — indistinguishable
from a real `m.set("absent", 0)`. This one *is* in the cheatsheet trap table; the
honest forms are `m.has(k): bool` (presence) and `m.get(k): Option<V>`
(`Some(v)` / `None`). Reach for those whenever "absent" and "present-with-zero"
must be told apart.

---

## Option, Result, and channels

### A discarded `Result` is silently dropped — there is no must-use **≈Go**

A `Result`-returning call as a bare statement compiles and runs; the `Err`
vanishes and the program continues:

```aril
strconv.atoi("not a number")     // Err is silently discarded
fmt.println("continued")         // runs
```

Aril's positioning is *"no exceptions — `Result<T, E>`"*, and Rust makes
`Result` `#[must_use]`, so this undercuts the promise more than it does in Go.
Until a discarded-`Result` diagnostic lands, treat a bare `Result` statement as a
smell — bind it and `match`, or propagate with `try` / handle with `catch`.

### `unwrapOr(fallback)` evaluates its argument eagerly **≈Rust, ≠ TS `??`**

The fallback is a normal argument, so it is computed *before* `unwrapOr` runs —
even when the receiver is `Some`/`Ok` and the fallback is discarded:

```aril
let n = strconv.atoi("5").unwrapOr(fallback())   // fallback() ALWAYS runs
```

This matches Rust exactly (which is why Rust ships a separate lazy
`unwrap_or_else`); it diverges from a TS developer's `??` / `?.` short-circuit
intuition. Keep the fallback cheap and side-effect-free, or guard with a `match`
when it isn't. *(Note: today these combinators resolve on a receiver whose type
comes from a signature — `atoi(...)`, `m.get(...)`, a typed return — but not yet
on one inferred straight from a `Some(...)`/`Ok(...)` constructor; write the
value through a `.get()`/typed boundary in the meantime.)*

### `.recv()` on a closed channel returns the zero value with no signal **≈Go, half of it**

Bare `.recv()` gives you the value half of Go's receive but drops the `ok` half,
so a drained/closed channel yields an endless stream of zeros with no closed
signal:

```aril
ch.send(42); ch.close()
fmt.println(ch.recv())   // 42
fmt.println(ch.recv())   // 0   — no indication the channel is closed
```

The honest form is `for v in ch { … }`, which **ends when the channel closes**.
Prefer the range loop over a hand-rolled `.recv()` loop.

---

## Concurrency

### A `var` mutated across `spawn`s is a silent data race **Aril-specific · sharp edge**

Aril's concurrency is uncolored and *structured* — a `scope` joins its `spawn`s
before it returns. But structured join is **not** capture safety: a `spawn`
closes over an outer `var` by reference, and concurrent mutation races on it,
producing a wrong, nondeterministic answer with no diagnostic:

```aril
var counter = 0
scope<unit, error> {
  for _ in 1..=1000 {
    spawn { counter = counter + 1; return Ok(()) }   // races on `counter`
  }
} catch e { return }
fmt.println(counter)                 // 955, 992, 990 … never reliably 1000
```

Rust's borrow checker rejects this outright; Go flags it with `-race` / `go vet`.
Aril does not reject it (yet — capture discipline is on the roadmap), so **do not
share a mutable binding across `spawn`s**. The correct patterns:

- **an atomic** — `atomic.Int64` and friends make the increment race-free (see
  [`atomics-lock-free.md`](atomics-lock-free.md));
- **a channel** — have each `spawn` send its contribution and fan them in on the
  receiving side.

To *detect* an accidental race, build or run with the race detector:

```
aril run -race  myprog.aril      # reports "DATA RACE" at runtime
aril build -race myprog.aril
```

`-race` forwards to Go's race detector (it needs a C toolchain / cgo). It is a
debug aid, not a fix — a clean `-race` run over your tests is evidence, not a
guarantee.

---

## `defer`

### `defer` in a loop accumulates to *function* exit, not per-iteration **≈Go**

`defer` runs at **function** exit, LIFO — not at the end of the enclosing block
or loop iteration. A TS `finally` or a Rust block-scoped `Drop` fires much
sooner, so a `defer` written in a loop body expecting per-iteration cleanup
instead stacks up N deferred calls that all fire together at return:

```aril
for i in 0..3 { defer fmt.println("d", i); fmt.println("b", i) }
// b 0   b 1   b 2         (loop body)
// d 2   d 1   d 0         (all defers, at FUNCTION exit)
```

That is a real resource-lifetime bug when the deferred call releases a
file/lock: N of them stay held until the function returns. If you need
per-iteration cleanup, pull the loop body into its own function (whose exit is
per-call) or release explicitly at the end of the iteration.

### `defer` is skipped by `os.exit`, but runs on `panic` **≈Go/Rust**

`os.exit(code)` terminates the process immediately and **does not run deferred
calls**; a `panic` *does* unwind through them. A file-close or lock-release
`defer` therefore silently no-ops on an `os.exit` path:

```aril
defer cleanup()      // runs on a normal return or a panic …
os.exit(1)           // … but NOT here — cleanup() is skipped
```

Don't rely on `defer` for cleanup you need before an `os.exit`; do it explicitly
before the exit call.

---

## Formatting and bindings

### `fmt.printf` verb mismatches produce a marker, not an error **≈Go**

A wrong format verb is not checked at compile time (Rust's `println!` is); it
produces Go's runtime format-error marker in the output:

```aril
fmt.printf("%d\n", "hello")    // %!d(string=hello)
```

The taught, checked path is `${}` interpolation, which takes any type — prefer it
over `printf` unless you specifically need C-style verbs.

### `math.round` is round-half-away-from-zero; JS's is not **≈Go**

`math.round` / `math.trunc` resolve and run, but a couple of members carry Go's
semantics, which differ from JavaScript's `Math`:

```aril
fmt.println(math.round(-2.5))   // -3   (Go rounds half AWAY from zero)
                                //      JS Math.round(-2.5) is -2 (half toward +∞)
fmt.println(math.trunc(2.9))    // 2    (toward zero)
```

The full bound `math` set is broader than the older documentation listed:
`sqrt abs pow exp log log10 log2 sin cos tan floor ceil round trunc hypot mod
min max` and the `pi` constant. Members outside that set (e.g. `cbrt`,
`remainder`, `signbit`) are unbound and give a clean `E0217` — drop to Go with an
`extern` if you need one.

---

## The reassuring half — priors that *do* transfer

Not everything surprises. These behave the way a careful developer hopes, and
several are *safer* than the Go/JS default — worth knowing so you don't
over-defend:

- **`Map`/`Set` iterate in insertion order, deterministically** — unlike Go's
  randomised map iteration. A `for (k, v) in m` is stable across runs.
- **Per-iteration loop-variable capture** — `for i in 0..3 { fns.push(() => i) }`
  captures `0 1 2` (like Rust / Go ≥1.22 / TS `let`), not the legacy-JS `3 3 3`.
- **Strict numeric typing** — `1 + 2.0` is a compile error (E0201); Aril even
  rejects Go's classic `var x: float64 = 5 / 2` integer-division gotcha. Mixed
  int/float and `int` vs `int64` comparisons are loud, not silent.
- **`==` compares tuples and records field-wise**; class instances need
  `refEq(a, b)` (loud E0401) — reference identity is never silently assumed.
- **`.map` on `None`/`Err` short-circuits** without invoking the closure;
  **`&&` / `||`** short-circuit correctly.
- **`match` is exhaustive and loud** — a missing case is E0303, an unreachable
  arm E0304.
- **Reversed range** `for i in 5..2` is simply empty (0 iterations), no panic.
- **`l.get(i)` returns `None` out of range** and **`s.copy()` / `l.toSlice()`
  produce genuine independent copies.**
