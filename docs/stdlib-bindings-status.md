# Standard-library bindings — current status

What of the Go standard library is reachable from Aril **today**, as opposed to
what the binding surface eventually intends. The intended *target* spelling of
every call lives in [`binding-surface.md`](binding-surface.md) (a contract the
binding generator must eventually satisfy); this file is the honest snapshot of
what is actually wired through the compiler right now, so a reader opening the
project can tell at a glance whether a package they need is usable yet.

Snapshot date: 2026-06-30.

## How a binding reaches Aril

A stdlib symbol is callable from Aril when it is part of the **builtin-module
surface** — an `import <pkg>` that the compiler recognises without an
`aril.toml`, lowering `pkg.method(...)` to the underlying Go call. There are two
ways a row gets there:

- **Mechanical** — the signature is derived from the Go type checker over a
  curated symbol list, with the standard wrapper rules (`(T, error)` →
  `Result<T, error>`, value/effect renames). One generated registry feeds both
  the type checker and code generation, so the two never drift.
- **Idiom** — a hand-authored wrapper that is *not* a mechanical signature
  transform: an effect that discards a Go `(int, error)` return, a runtime
  helper, or a Go type conversion. These are the deliberate carve-outs the
  mechanical deriver excludes.

Anything outside the builtin-module surface is **not yet available**: a program
importing it does not build. (Third-party Go packages reached through the
explicit `extern` FFI layer are a separate path, not covered here.)

## Bound today

| Package | Bound symbols | Kind |
|---|---|---|
| `fmt` | `println` `print` `printf` (effects) · `sprint` `sprintf` `sprintln` · `errorf` (`%w` wrapping) · `scan` `scan2` `scan3` (stdin) | idiom + mechanical |
| `os` | `args` `exit` `getenv` `readFile` `writeFile` · `lookupEnv` (→ `Option`) | mechanical + idiom |
| `strings` | `contains` `count` `fields` `hasPrefix` `hasSuffix` `index` `join` `replace` `split` `splitN` `toLower` `toUpper` `trimPrefix` `trimSpace` `trimSuffix` · `fromBytes` `toBytes` | mechanical + idiom |
| `strconv` | `atoi` `formatBool` `formatFloat` `itoa` `parseBool` `parseFloat` `parseInt` `quote` | mechanical |
| `math` | `abs` `ceil` `cos` `exp` `floor` `hypot` `log` `log10` `log2` `max` `min` `mod` `pi` `pow` `round` `sin` `sqrt` `tan` `trunc` | mechanical |
| `unicode` | `isDigit` `isLetter` `isSpace` | mechanical |
| `slices` | `max` `min` `contains` `indexOf` (→ Go `slices.*`) · `reverse` `dedup` (new copy) | idiom |
| `time` | `after` `sleep` `tick` · `seconds` `milliseconds` (duration ctors) | mechanical + idiom |
| `sort` | `sorted` (comparator) · `sortedBy` (key extractor) — return a new slice | idiom |
| `json` | `parse<T>` `serialize` `serializeIndent` (+ `Option` ⇄ `null` round-trip) | idiom |
| `errors` | `is` (sentinel/chain classification) · `new` (constructor) | mechanical + idiom |

Predeclared free functions `min` / `max` (generic over any ordered type — int,
float, string) fill the int-extremum gap `math.min` / `math.max` (float64-only)
leave; they lower to Go's builtin `min` / `max`.

## Not yet available

Grouped by how close each is to landing — the upper rows are mostly small
additive bindings, the lower rows need translator work or are whole subsystems.

**Small surface gaps (a missing method on an already-bound package).**
The std streams `os.stdout` / `stderr` (`os.stdin` is bound — the reader
`bufio.newScanner` consumes).

**Error inspection.** Construction (`errors.new` / `fmt.errorf` with `%w`
wrapping), folding into `Result<T, error>`, and *classification* (`errors.is`,
which walks the wrapped chain) are bound. The remaining gap is typed
*unwrapping* — `errors.as<T>` (extract a concrete error type to `Option<T>`):
the comma-ok→Option lift it needs now exists (`os.lookupEnv` uses it), but
`errors.As`'s pointer-out protocol is a distinct, harder shape.

**Value-typed handles and bound methods.** Packages whose surface is a type
carrying methods. **Bound:** `regexp.Regexp` (`mustCompile` + `matchString` /
`findAll`) — an external Go-package handle; and `big.BigInt` (`fromInt` /
`fromInt64` + `add` / `sub` / `mul` / `div` / `toInt64`) — a runtime-backed
functional wrapper over `math/big`'s pointer-mutation API (each op returns a
fresh value). And `bufio.Scanner` (`newScanner(os.stdin)` + `scan` / `text`) — another external
handle, the line-by-line stdin reader. And `time.Duration` arithmetic
(`add` / `mul`, lowered to Go operators since `Duration` is a scalar, +
`string`) over the existing `time.seconds` / `time.milliseconds` constructors.
All surface through the builtin-module namespace, their method sets dispatching
on the handle's boundary type. Still on the target surface: `time.Time`
(clock/formatting), `bufio.Writer`, and the wider `big` surface
(`mod` / `cmp` / `string`).

**Bound interfaces with enforced conformance.** `io.Reader` / `Writer` /
`Closer` and friends. A class may *write* `implements io.Reader` today, but the
clause is not method-set-checked and is dropped in lowering — so the interfaces
are part of the target surface, not yet a wired, enforced binding.

**Synchronisation.** `sync.WaitGroup` / `Mutex` / `Once`. (The uncolored
`scope` / `spawn` / channel concurrency model covers many cases that would reach
for these in Go; a shared-memory mutex has no working construction path today.)

**Networking and the web.** `net/http`, `net`, `net/url` — a whole subsystem,
its own future effort. The HTTP examples in the corpus are illustrative
sketches that do not build.

**Out of scope until a cold-start strategy decides.** `database/sql` (the
canonical hard binding), `os/signal`, `crypto/*`, `compress/*`, `image/*`,
`text/template`, `html/template`. `reflect` / `unsafe` / `runtime` / `syscall`
are owned by the binding generator with no Aril-side surface planned.
