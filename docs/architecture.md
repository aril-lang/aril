# Architecture

How the Aril compiler is built. This file is the concrete "how"; the *why*
behind the architectural commitments (D1, D6, D8, D10, D14, D15, D16, ...)
lives in [`docs/design-decisions.md`](design-decisions.md).

## 1. Compiler pipeline

```
.aril source
    │
    ▼
  lexer        internal/lexer    — text -> tokens
    │
    ▼
  parser       internal/parser   — tokens -> AST
    │
    ▼
  AST          internal/ast      — typed syntax tree
    │
    ▼
  checker      internal/sema     — type checking, inference, exhaustiveness
    │
    ▼
  codegen      internal/codegen  — Aril AST -> Go source (+ //line directives)
    │
    ▼
  go build     (the Go toolchain) — Go IR -> native binary
```

The Aril compiler's job ends at emitting Go. The Go toolchain produces the
binary.

### Build-artifact layout

`aril build`/`run` write into a per-project **`aril-out/`** directory (visible,
auto-git-ignored via an `aril-out/.gitignore` = `*`): the binary lands at
`aril-out/bin/<name>` and the lowered Go persists in `aril-out/gen/`. Persisting
`gen/` (rather than lowering to a throwaway temp dir) keeps the source path
stable so an unchanged rebuild hits Go's `$GOCACHE` — Go stays an IR the
developer never works in (D1), its on-disk presence incidental. `gen/` is kept
in sync with the sources by an emitted-files manifest (a removed source's `.go`
is pruned, or `go build` would compile a phantom). The output directory is
resolved by, in order, `--out-dir` › the `ARIL_OUT` env var › a `[build]
out-dir` manifest key › the default `./aril-out`; an explicitly-set (shared)
out-dir is namespaced per project (`<out>/<project-id>/…`). A build takes an
exclusive lock on the out-dir for the span of lowering + `go build`. `aril
clean` removes the directory. The dependency cache is separate and global
(`$ARIL_CACHE`). The REPL, being projectless, keeps a throwaway temp module.

## 2. Go as the intermediate representation

Go is an IR (decision D1), with these consequences:

- Codegen is not constrained to produce idiomatic or readable Go.
- Sum types, exhaustive `match`, and non-nullable types are encoded for
  correctness — typically as sealed-interface families plus tag discrimination.
- Calling a bound Go function is just an emitted Go call: **zero** runtime FFI
  cost, no marshalling, no ABI boundary.

### Source maps — `//line` directives

`internal/codegen` emits `//line file:line` for every construct. The Go
compiler honors these, so runtime panics, stack traces, `runtime.Caller`, and
(via DWARF) `delve` stepping and `pprof` profiles all report `.aril` locations.
Mandatory from Phase 1 (decision D8).

### The Go subset contract

Aril commits to a defined, stable subset of Go as its IR (decision D15). Codegen
emits only that conservative subset and never depends on experimental Go
features. Bindgen depends only on the stable `go/importer` / `go/types` API
(source-mode importing, keeping the toolchain dependency-free), never on
compiler internals. The exact subset and supported Go version range
are a backlog item to be pinned before codegen settles.

## 3. The binding subsystem — `internal/bindgen`

```
Go package
    │
    ▼
go/importer introspection    — load real types and signatures (source mode)
    │
    ▼
raw binding declarations     — mechanical, signature-faithful (D6)
    │
    ▼
idiomatic wrapper pass       — agent/human: (T,error)->Result, options, etc.
    │
    ▼
std/<package>.aril             — generated, committed bindings
```

Raw signatures are derived from the Go type checker and never hand-written.
Only the wrapper layer involves judgment.

### Two realizations: the `extern` files and the builtin-module registry

The pipeline above feeds the **`extern` / `aril import`** world — a Go package a
program binds explicitly with `extern func … @go("pkg.Sym")`, where `aril
import` prints a curatable `.aril` starting point. The **builtin-module** stdlib
surface (`import fmt` / `fmt.println(x)`) is the *same idea, different output*:
its mechanical binding facts (the Go referent name, the rename-vs-`(T,error)`
lowering shape, and the Aril return type) are derived from the same
go/importer introspection over a curated symbol manifest and committed as a
generated **Go data registry** (`internal/binding`, derived by
`internal/bindgen`) that both `internal/sema` and `internal/codegen` read — one
source for the typing and the lowering, so they cannot drift. The registry is
pure data, so the compiler build needs no Go source tree; only regeneration
does, guarded by a drift test. The *idiom* bindings that are not mechanical
signature transforms — fire-and-forget effects (`fmt.print*`, whose Go
`(int, error)` is deliberately discarded), the runtime-helper bindings
(`fmt.scan*`, `json`, `sort.sorted`), the time-duration constructors, and
`strings.fromBytes` — stay hand-authored in the consumers: the "wrapper layer
involves judgment" rule, made concrete. (The committed-`std/<package>.aril`
form above is the longer arc; the Go-data registry is the realized MVP.)

**Binding membership is consolidated in `internal/binding`** (`membership.go`).
The idiom rows that stay hand-authored still split into two flavours: the
*value* tables whose data has no codegen dependency (the rename overlay for
`fmt.print*`/`log.*`/`slices.max`/…, the `strings.fromBytes` conversion table,
the `os.lookupEnv` comma-ok table, the time-duration constructors) live in
`internal/binding` so `internal/sema` and `internal/codegen` share them; the
rows whose lowering *is* code (the `json`/`fmt.scan`/`errors.as` codegen
intercepts, the `sort`/`slices` runtime helpers, and the codegen-owned
`reflect` surface) keep their code in the consumer but register their
*membership* in `binding.idiomMembers`. `binding.IsMember(pkg, arilName)` folds
every source — registry, overlays, handle constructors, idiom members — into
one predicate. Its single consumer is sema's unbound-member diagnostic: firing
"member not bound" requires the *complete* bound set, and the pre-consolidation
overlays lived in `internal/codegen`, invisible to sema. The predicate is
sound-over-complete — a member missing from `idiomMembers` suppresses the
diagnostic (leaks as before), never rejects a working call — so a new codegen
intercept must add its `idiomMembers` row in lockstep, checked by the corpus
ratchet (a false reject drops `build_ok`). The diagnostic gates on the sema
`SymBuiltinModule` symbol, which only the shipped predeclared namespaces
produce, so a user's own `extern`/`@go` binding (a distinct `SymExtern*`
symbol) is never mistaken for an unbound stdlib member.

### Mapping rules (Go -> Aril)

| Go shape | Aril shape | Notes |
|---|---|---|
| `func F() (T, error)` | `Result<T, error>` | the dominant error idiom |
| `func F() (T, bool)` | comma-ok form / `Option<T>` | distinct from the error case |
| `func F() *T` returning nil | `Option<T>` | nil-returning pointers are nullable |
| `func F() *T`, always non-nil¹ | `T` (direct) | wrapper-layer override; audited per binding |
| `func F() T`, T valid with error (e.g. `Read`) | tuple `(T, error)` kept | rare; do not collapse |
| `func F() (T, U)` (neither error nor bool) | tuple `(T, U)` | e.g. `context.WithTimeout` |
| `interface{}` / `any` | `Any` (escape type) | binding-only; concrete values widen at call site |
| `type D int64` (named) | nominal newtype | working shape `newtype D = int64` |
| variadic `...T` | variadic param `...T` | for `...any` → `...Any` |
| variadic `chan<- T` / `<-chan T` | `SendChan<T>` / `RecvChan<T>` | directional channel views |
| functional-options pattern | normal optional params | wrapper-layer work |

¹ The wrapper layer marks a returned `*T` as non-nullable when the Go
documentation or established usage guarantees a non-nil pointer
(`*http.Request.URL`, `*http.Request.Body`, `*http.Response.Header`,
...). The audited list is one of the L3 "this is where a wrapper made a
semantic decision" checkpoints; mismatches with reality become bugs and
are caught by differential testing.

The dangerous cases — nullability, `(T,error)` vs comma-ok vs both-valid,
panics — are *semantic*; signature derivation does not catch them. They are the
focus of behavioral testing (section 6).

### The FFI wall

Go packages enter Aril *only* through generated bindings (D4). They are not
first-class imports. This keeps Go's `nil` / pointer / `interface{}` /
`context` impedance at one explicit boundary.

## 4. Concurrency

Concurrency is uncolored (decision D7) — no `async`, no `await`.

- `spawn { ... }` — run a block concurrently (compiles to a goroutine).
- Channels — `makeChannel<T>()`, `.send(v)`, `.recv()`, typed `chan<T>`.
- `select` — wait on multiple channel operations.

The recommended surface is a **structured-concurrency scope**: tasks spawned in
a scope are joined when it ends; the first failure cancels its siblings. One
concept covers fan-out, error propagation, and cancellation, and maps onto Go
as `errgroup` + `context`. Cancellation derives from scope lifetime; explicit
`context` values from bound APIs interoperate at the binding boundary.
Generated concurrent code must pass `go test -race`.

## 5. Module system

Aril packages resolve with a decentralized, go-mod-style scheme (D5): import
path as URL, no central registry, MVS-style version selection, vendoring. Go
packages do not resolve this way — they enter only through bindings (D4).

## 6. Testing — the layered ladder

Cheapest checks first; do not spend expensive checks where cheap ones suffice.

- **L0 — impossible by construction.** Bindings generated from `go/importer`
  type info (D6): signature bugs eliminated, not tested.
- **L1 — round-trip compilation (free).** Every binding plus a use site is
  compiled Aril -> Go -> `go build`. Bad symbols are rejected by the Go
  compiler itself. Run across multiple Go versions to catch API drift.
- **L2 — structural diff against `go/types`.** Assert each binding is a
  faithful transform of the real signature under the mapping rules. Side
  effect: enumerates every place the wrapper made a semantic decision — an
  explicit audit list.
- **L3 — behavioral / differential testing.** Call the same Go function
  through the binding and directly, on fuzzed inputs, and diff. Fuzzing drives
  functions into nil/error branches where bugs live. For panics and nil: the
  agent predicts from the doc comment; a test confirms the prediction.
- **L4 — Go `Example*` functions as oracles.** Black-box, maintained,
  independent of Aril's generator. Convert these (not white-box unit tests)
  and check `// Output:`. Necessary but not sufficient: `Example*` functions
  cover documented, happy-path usage — error and nil branches are covered by
  L3, not L4. The two layers are complementary.
- **L5 — free static/dynamic oracles.** `go vet` on generated code;
  `go test -race` for concurrency.

Generated Go must always pass `go build`, `go vet`, and — for concurrent code —
`go test -race`. Compatibility scoring: anchor the denominator to Go's symbol
surface, tag depth (smoke / differential / fuzzed), score per
(package x category). The recurring failure mode is a test plan heavy on
"does it compile / type-check" and light on "does it *behave* identically" —
the dangerous binding bugs (wrong nullability, `(T, error)` vs comma-ok
confusion) pass compilation. Behavioural / differential testing on fuzzed
inputs is first-class, not an afterthought.
