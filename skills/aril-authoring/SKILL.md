---
name: aril-authoring
description: Write idiomatic, contract-bearing Aril — reach for the type system (sum types, Option/Result), author contracts that fire on a broken impl, avoid the compiles-but-differs-from-TS traps, and drop to Go when a binding is missing. Use whenever writing or editing .aril source.
---

# Authoring Aril — the recommended style

Aril has TypeScript-style syntax over an ML-family type system (sum types,
exhaustive `match`, `Option`/`Result`), compiled to Go. Writing Aril that
*compiles* is not the bar — this skill is about writing the **recommended**
Aril: idiomatic, trap-free, and carrying contracts that catch your own mistakes.

**Reference chain (read in this order, look things up — don't guess):**
- **`docs/cheatsheet.md`** — the one-page surface + the TS/Go/Rust trap table.
- **`docs/stdlib-bindings-status.md`** — *which* stdlib is bound today + method
  sets. The API is lookup-able, not intuit-able: never invent a call spelling.
- **`docs/language-spec.md`** — the full language surface, incl. Contracts.
- **`aril emit <file.aril>`** — see the Go your code lowered to (`-no-line` for a
  clean read). Go is the IR; you can always inspect the cost.

## 0. Prime directive: verify, don't trust

Aril is a new language with little training corpus, so a first draft is often
subtly wrong. Close the loop every time:

1. `aril build <file>` — a binding you assumed may not exist. If it fails, the
   diagnostic is in **`.aril` coordinates** (Aril terms, an `E0xxx` code) — read
   it; it is not a raw Go error.
2. `aril run --contracts=panic <file>` — run with contracts **on**. A firing
   contract indicts the code at its exact Aril site — that is the point.
3. If a binding is missing, don't fake it — bind it (§6) or pick a bound API
   (look it up in `stdlib-bindings-status.md`).

## 1. Reach for the type system

- **Model states as a sum type + exhaustive `match`**, never a sentinel `int`,
  a bare `bool` flag, or a stringly-typed tag. `match` is exhaustive (E0303
  names the missing case), so an added variant forces every site to handle it.
  ```aril
  type Token = | Num(value: int) | Plus | Minus
  match tok { Num(v) => …, Plus => …, Minus => … }   // no default needed or wanted
  ```
- **Absence is `Option<T>`** (`Some`/`None`), never a null/zero/-1 sentinel.
  **Failure is `Result<T, E>`** (`Ok`/`Err`), never an exception or an error
  code. There is no `null`, `nil`, `undefined`, or `throw`.
- **Consume with `match`** (or the total methods `unwrapOr`/`isSome`/`isOk`);
  there is no panicking `.unwrap` and no `.map` in v1.

## 2. Error handling — the three tools, and when

- `try e` — propagate the error unchanged (the common case; Rust's `?`). Legal
  only in a `Result`/`Option`-returning function.
- `try e.mapErr((err) => Other{…})` — propagate **across an error-type
  boundary** (your function's `E` differs from `e`'s). Keeps a plain `try`.
- `e catch err { … }` — only when there is **no `Result` to propagate into**.
  The handler **must diverge** (`return` / `os.exit` / `panic`) — it cannot fall
  through with a value. To recover *and continue*, use `unwrapOr`. To
  distinguish error kinds, make `E` a sum type and `match err` inside the
  handler (each arm still diverging).

## 3. Author contracts that fire (the differentiator)

Contracts (RFC-0006 value, RFC-0007 channel) are an executable spec written
*beside* the code, enforced under `--contracts=panic`, elided otherwise. For an
agent this is high-leverage: the contract is a machine-checkable **target** and
a self-**oracle** that localizes your own bug. Turn the task's stated property
*into* a contract.

**The rubric: a good contract fires on a broken implementation.** Pin the
load-bearing property, not a tautology.

```aril
// GOOD — rules out the trivially-wrong impls (identity, subtree-drop, corruption):
contract invert { ensures seqEq(inorder(result), reversed(inorder(t))) }

// GOOD — the classic "bumped one counter but not the other" bug:
contract LRU {
  invariant size >= 0
  invariant size <= capacity
  invariant size == index.len()
}

// BAD — always true, catches nothing:
contract f { ensures result == result }
```

- `requires` = precondition; `ensures` = postcondition (`result` names the
  return; snapshot pre-state in an `entry { let … }` prelude); `invariant` =
  a class/type property re-checked at construction **and every method exit**.
- Draw predicates from the bundled `std/pred` (`seqEq`, `reversed`, `isSorted`,
  `allDistinct`, …) and write small domain predicates as ordinary functions.
- Channel protocol: `channel ch { forbid send after close }` turns a
  send-after-close bug into the controlled diagnostic E1203, not a raw panic.

## 4. Idiom-conformance — avoid the works-but-wrong traps

These compile but a TS/Go habit gets them wrong (full list: the cheatsheet trap
table). Write the Aril idiom:

- Slice `xs.push(e)` **returns a new slice** — write `xs = xs.push(e)` (a bare
  `xs.push(e)` is E0215). (`Stack.push` mutates — different type, deliberate.)
- Map miss: use `m.get(k): Option<V>`, not `m[k]` (which yields the zero value).
- Class-instance equality: `refEq(a, b)` (E0401 on `==`); records/tuples compare
  field-wise. Records are **nominal** in v1 — same-shape ≠ interchangeable (E0201).
- `string → int` is not a cast (E0205) — `strconv.atoi` (Result) / `itoa`.
- Interfaces are **nominal**: `class C implements I { … }` (no structural match).
- Channels are method-based: `ch.send`/`ch.recv`/`ch.close`; `<-ch` is
  select-only.
- Top-level functions use `func`; there is no `fn`/`function`. No top-level
  `var` — module mutable state lives in a `class` instance.

## 5. Concurrency — uncolored and structured

No `async`/`await`, no bare `go`. Use a `scope`:

```aril
try scope<unit, error> {
  for w in 1..=n { spawn { worker(w, jobs, out); return Ok(()) } }  // spawn returns Ok(())
}
```

- The scope **joins before it returns**; code after it (and its trailing
  expression) can rely on every spawn having finished.
- A fan-in channel drained *inside* the scope must be **buffered ≥ spawn count**.
- Propagate cancellation with `scope.context` (pass it to callees that open
  inner scopes as `scope<T,E>(ctx) { … }`).

## 6. When a binding is missing — drop to Go

Aril binds (does not port) the Go stdlib. If `stdlib-bindings-status.md` doesn't
list what you need, bind it with `extern` + `@go`:

```aril
extern type Cmd @go("os/exec")
extern func command(name: string, args: []string): Cmd @go("os/exec.Command")
extern impl Cmd { output(): Result<[]byte, error> @go("Output") }
```

Generate a starting binding from any package with **`aril import <go/path>`**
(`--from <module-dir>` for a non-stdlib module). The boundary lifts
`(T, error) → Result<T, error>` and `(T, bool) → Option<T>` for you; the emitted
Go is re-typechecked by `go build`, so a drifted binding fails loudly.

## Pre-submit checklist

1. **Builds** — `aril build` clean (no assumed-but-missing binding).
2. **Runs** — `aril run --contracts=panic` produces the expected output.
3. **Contracts fire** — each `contract`/`channel` pins a real property (would
   trip on a broken impl), not a tautology.
4. **No invented API** — every stdlib call was looked up in
   `stdlib-bindings-status.md`.
5. **Idiomatic** — no trap from §4; states modelled as sum types; absence/failure
   as `Option`/`Result`.
