# RFC-0010 — Module-aware bindgen: raw-Go and binding-package dependencies

| Field | Value |
|---|---|
| Number | 0010 |
| Status | implemented |
| Created | 2026-07-07 |
| Supersedes | — |

## Summary

RFC-0008 defines three dependency kinds and ships the first: `kind = "aril"`, a
pure-Aril library whose source compiles into the one emitted Go module. This RFC
designs the remaining two — the half that lets **Go's ecosystem bootstrap Aril**:

- **`kind = "go"`** — a raw Go module the consumer does not own, bound through a
  **consumer-authored binding table** that drives `aril import` *module-aware*
  over the fetched module and is **auto-consumed** into the build.
- **`kind = "binding"`** — a **published `.go`→`.aril` binding package**: a
  distributable, versioned module that ships curated `extern` bindings plus a
  self-declaration of the Go module and version it binds. Strictly *sugar* over
  `kind = "go"` — a published table rather than a consumer-owned one.

Both introduce a Go-level `require` + `replace` (target: the fetched cache),
riding the machinery that binds a vendored third-party module. The north star is
`database/sql` with a real driver (`github.com/lib/pq`).

The one architectural fork this RFC resolves is **how to introspect a fetched
third-party module's exported API** — the D22 loader question, which the arrival
of a module graph forces. This RFC keeps the compiler dependency-lean: it
introspects the fetched module with the **stdlib type-checker inside a
synthesized module context**, isolated behind an internal loader interface so the
choice stays reversible, and names the robustness ceiling and its stdlib-only
successor.

## Motivation

Three durable problems, each unreachable with `kind = "aril"` alone.

1. **Cold start (Open Problem #1).** Binding the stdlib gives a usable language,
   not a web framework, ORM, or drivers. The decentralized ecosystem (D5) starts
   empty. The only way it fills without hand-porting the world is to let Aril
   *consume Go code directly* — bind an arbitrary Go module by import path and
   use it. `kind = "aril"` composes Aril source; it cannot reach a Go library.

2. **The hard bindings need a driver (Open Problem #2).** `database/sql` is the
   canonical hard binding, and it is *not stdlib-only*: a working database needs a
   third-party driver (`github.com/lib/pq`), a raw-Go external dependency. It is
   unreachable until a raw-Go dependency kind and a module-aware `aril import`
   exist. The DB-with-driver case is the concrete north star that keeps the design
   honest — an adult-ecosystem problem at zero external users.

3. **A published binding is how a decentralized ecosystem shares Go reuse.** A
   consumer-owned table binds Go for *one* project; it does not compose. The way a
   TypeScript project reuses a JS library it does not own is a published
   declaration package (`@types/*`); Aril's peer is a published binding package
   that a second consumer depends on the way it depends on any module. Without it,
   every project re-authors the same `database/sql`-driver table.

The reserved half exists in shape: `aril.toml` parses `kind`, `path`, `binds`,
`binds-go`; the manifest resolver classifies a dependency's kind; the `extern` +
`@go` FFI surface lowers a bound Go call. What this design supplies is the
*behaviour* behind the reserved fields — fetch, introspect, emit.

## Design

### Overview — one live third-party path, driven by the manifest

Two third-party mechanisms exist and this RFC **joins them**. A build already
binds a third-party Go module by emitting a `require` + `replace` into the
generated `go.mod`, keyed off the `@go("module/path.Sym")` import paths the
lowered Go contains — but driven by a hand-authored side manifest and a *vendored*
tree, not by `aril.toml`. Separately, the RFC-0008 resolver classifies a
dependency's `kind` from `aril.toml` but resolves only `kind = "aril"`, refusing
`go`/`binding` with a *not-yet-wired* diagnostic. The design makes a
`kind = "go"`/`"binding"` dependency flow through the resolver into that same
`require`+`replace` emission, with the **fetched cache** as the `replace` target:

```
   aril.toml [dep] kind=go/binding
        │  aril get: fetch the Go module + (binding) the package into the cache
        ▼
   $ARIL_CACHE/<source>@<version>/        (the fetched module tree)
        │  aril build/run (offline):
        ├─ kind=go:      load the module (module-aware) → aril import over the
        │                consumer table → extern bindings → compiled in
        ├─ kind=binding: the published extern bindings compiled in (kind-1-like)
        └─ both:         emit `require <bound-go-module> <binds-go>` +
                         `replace <bound-go-module> => <cache dir>` into go.mod
```

The emitted `extern func … @go("module/path.Sym")` / `extern type` / `extern impl`
surface is unchanged — it is already third-party-capable and already lowers a
foreign call correctly. The new work is upstream of it: fetch, introspect, and
generate that surface for a module the consumer names.

### Module-aware loading — introspecting a fetched Go module

`aril import` and the registry deriver type-check a Go package from source with
the stdlib `go/importer` in *source* mode (D22 — the compiler carries no
`golang.org/x/tools` dependency). Source mode resolves a **stdlib** package
directly; it does not, on its own, walk a module graph — with no module context
it can reach only GOROOT.

The design keeps D22's stdlib-only posture by giving the stdlib type-checker the
**module context it needs**, synthesized from the fetch the resolver already
performs. To introspect a fetched module `M@V` the loader:

1. Materializes a throwaway Go module directory whose `go.mod` **requires `M`**
   and **`replace`s `M` to the cache tree** `$ARIL_CACHE/<source>@V/` — the exact
   `require`+`replace` shape a build already emits, and a `replace` needs no
   network.
2. Writes a **blank-import anchor** (`import _ "M/pkg"`) for each package the
   consumer table (or the published binding) names, so the target is in the build
   graph.
3. Runs the stdlib source-mode importer against `M/pkg` inside that directory. It
   resolves the package through the `replace`, type-checks it from source, and
   yields a `*types.Package` whose exported scope is the binding surface. (An
   empirical probe on go1.22 confirms source-mode resolution through a `replace`
   target — the mechanism this step relies on; loader reproducibility across Go
   versions is the one property to re-confirm before implementation.)

This reaches the north star: `github.com/lib/pq` is pure Go, and the north-star
API surface (`database/sql` + a driver registered via a blank import) type-checks
from source this way with no new compiler dependency.

**The loader is isolated behind an internal interface** (`moduleLoader`:
import-path → `*types.Package`), so its implementation is swappable without
touching bindgen or the resolver. This matters because the source-mode approach
has a **named ceiling**: it leans on `go/build`'s module resolution, which the
stdlib documents as legacy for general module-aware use, and it cannot see
**cgo-defined** API entities (a cgo driver's exported surface is partially
invisible to source-mode type-checking). When a target module crosses that
ceiling, the loader is replaced *behind the same interface* by the stdlib-only
successor — driving `go list -json -deps` (the same primitive the community
loader is built on) and type-checking its output with `go/types`, which handles
cgo-preprocessed files and complex build graphs while still adding **no**
third-party dependency to the compiler. Adopting `golang.org/x/tools/go/packages`
outright remains an explicit escape hatch (a D22/D19 amendment), deliberately not
taken here. See *Alternatives*.

### `kind = "go"` — the consumer-owned binding table

A `kind = "go"` dependency names a raw Go module (`source`, `version`) and a
consumer-owned table (`path`). The table drives module-aware `aril import` over
the fetched module and its output is auto-consumed. Having no `aril.toml`, a
`kind = "go"` module self-declares nothing and is a **leaf** of the Aril MVS
graph; its transitive *Go* dependencies resolve through Go's own
`require`/`replace`, not Aril resolution.

The table's vocabulary is the minimal intersection that consumer-owned FFI
systems (Kotlin cinterop `.def`, SWIG `.i`, Rust `bindgen`) independently
converged on, minus the C-toolchain concerns the Go toolchain makes irrelevant.
It is written in Aril's own reserved syntax — the mechanical bindings the table
*selects* are exactly `extern` declarations, so the table is authored as an Aril
module whose `extern` items are the request:

- **Select** — the table names the Go symbols to bind (functions, types, methods)
  by declaring an `extern` for each, with `@go("module/path.Sym")`. Selection is
  explicit and closed: only named symbols are bound. (Rank-1 across all prior art;
  Aril's `extern`-as-request folds selection and the emit target into one form.)
- **Rename** — the Aril name of an `extern` is the local spelling; `@go` carries
  the divergent Go name. A `package`-level import root groups the surface
  (`import pq/…`). (Rank-2; SWIG `%rename` + Kotlin `package`.)
- **Retype / shape** — the `extern`'s declared Aril types shape the surface: a Go
  `(T, error)` return declared `Result<T, error>`, a comma-ok `(V, bool)` declared
  `Option<V>`, an opaque Go type declared `extern type` (lowers to `*pkg.Sym`).
  (Rank-3; the enum/opaque axis, expressed through Aril's own types.)
- **Error/nullability annotation** — the highest-leverage Go-specific axis: the
  table states, per binding, how a Go `error` / nil / `(T, error)` maps into an
  Aril `Result`/`Option`. This is the fact a mechanical binder cannot infer
  (SWIG `%newobject`, bindgen `opaque_type` analog) and where the `database/sql`
  ergonomics live.
- **Manual-shim escape** — every mature FFI system has a "the generator cannot
  express this — hand-write a shim" trapdoor (Kotlin's `---` block, cffi's
  `set_source`). A `kind = "go"` project may place a hand-written Go shim file in
  the table directory, compiled into the emitted module alongside the generated
  bindings, for the surface `extern` cannot spell (a variadic flattener, an
  `interface{}` adapter).

The table is **validated against the pinned module at load time**: an `extern`
naming a Go symbol that is absent or shape-changed in `M@V` is a **loud
Aril-coordinate diagnostic (E0126)**, not a silently-shrunk surface — the
improvement over all prior art surveyed, which lets a table entry for a deleted
symbol pass silently. Because Aril already pins `M@V` (MVS + `aril.lock`), the
table + lock pair makes regeneration reproducible and the table a checked
*contract* against a specific version.

### `kind = "binding"` — the published binding package

A published binding package is a distributable module (its own `source`,
`version`, `aril.toml`) that self-declares `kind = "binding"`, `binds` (the bound
Go module path), and `binds-go` (the bound Go version — a floor). It ships the
`extern` bindings as ordinary Aril source (kind-1-like, compiled in) *and* causes
a `require <binds> <binds-go>` + `replace <binds> => <cache>` for the bound Go
module. It is **sugar over `kind = "go"`**: the same generated `extern` surface,
authored and published once rather than re-derived per consumer.

Prior art (TypeScript `.d.ts` + DefinitelyTyped, the strongest analog) fixes four
design points:

- **The bound `module@version` is first-class, required manifest data.** Every
  system that omits it (Kotlin `.def`, ReScript, Scala.js facades) suffers silent
  drift with no way to answer "which library version was this written against."
  Aril's `binds`/`binds-go` already are that data — kept required for a binding
  package.
- **Independent binding-revision axis.** DefinitelyTyped versions a declaration
  package as `major.minor` of the library + an *independent* patch line for fixes
  to the binding itself. Aril's binding package carries its own `[package]
  version`-equivalent (its git tag) tracking the binding's own revisions, while
  `binds-go` tracks the bound Go module — two axes, so a binding fix ships without
  a Go release. MVS keys on the binding package's own version.
- **One canonical binding table per Go module.** npm's `@types/foo` naming and
  Kotlin's one-`.klib`-per-`.def` give de-facto uniqueness; Scala.js's *competing*
  facades per library are the fragmentation counter-example. Aril enforces it
  (E0124, below): at most one binding table — a `binding` package or a
  `kind = "go"` table — may bind a given Go module across a build graph.
- **Prefer inline-with-library; third-party is the drift-prone fallback.** The TS
  ecosystem trends toward libraries shipping their own types. A binding package is
  the fallback for a Go module whose author does not publish an Aril binding; the
  design does not privilege it over a first-party table, and validates both
  against the pinned Go module the same way.

### The `require` + `replace` wiring and the Go-version floor

For every `kind = "go"`/`"binding"` dependency, the emitted `go.mod` gains a
`require <bound-go-module> <version>` + `replace <bound-go-module> => <cache dir>`.
Two axes the current vendored path hardcodes become live:

- **`binds-go` is honored.** The `require` version is the binding's self-declared
  `binds-go` floor (a `kind = "binding"` package) or the consumer's `[dep].version`
  / `binds-go` override (a `kind = "go"` module, or a consumer raising a binding's
  floor). Because every Go-binding dependency lowers into the *one* emitted Go
  module, Go's own module resolution takes the **max** `binds-go` across all
  binding dependencies — so a binding may run against a Go module newer than the
  one it was generated against with no consumer action. That implicit drift earns
  a **warning (E0125)**, not silent success.
- **The Go toolchain version.** The emitted `go 1.x` directive is the max of the
  root's `[toolchain] go` and any dependency floors (RFC-0008 §Compatibility axes),
  rather than a hardcoded constant — wiring the deferred RFC-0008 carry-forward,
  now that real Go-binding dependencies exist to contribute a floor.

### Binding-uniqueness and implicit-`binds-go` drift diagnostics

- **E0124 — duplicate binding of a Go module.** Two binding tables (any mix of
  `binding` packages and `kind = "go"` tables) binding the *same* Go module across
  the build graph is a hard error: two tables would emit duplicate `extern`
  declarations of the same Go symbols into the one lowered module (the Cargo
  `links` invariant). Names both binders; the fix is to drop one or alias through
  a single table.
- **E0125 — implicit `binds-go` drift (warning).** Go-level max-of-floors raised a
  bound Go module above a binding's self-declared `binds-go`. The binding runs
  against un-tested-against bytes (ABI-drift risk); the warning names the binding,
  its declared floor, and the resolved version.
- **E0126 — binding table names an absent/changed Go symbol.** A table `extern`
  (or a published binding) references a Go symbol not present, or shape-changed, in
  the pinned `M@V`. A loud Aril-coordinate diagnostic pointing at the offending
  `extern` — the table is a contract against the pinned version, and drift fails
  closed rather than shrinking the surface silently.

`kind = "go"` also refines the reserved E0121: a declared `go`/`binding`
dependency present in the cache but not yet introspected resolves; only a genuine
resolution failure (absent from cache, missing table, unreadable module) keeps
E0121.

### Diagnostics (Aril-coordinate, D10)

New allocations in `diagnostics.md`: **E0124** (duplicate binding of a Go module,
error), **E0125** (implicit `binds-go` drift, warning), **E0126** (binding table
names an absent/changed Go symbol, error). E0121/E0122/E0123 are unchanged; E0121
stops firing for a resolvable `go`/`binding` dependency.

## Alternatives considered

Grounded in prior-art passes over published-binding formats (TypeScript
`.d.ts`/DefinitelyTyped, ReScript `external`, Kotlin/Native cinterop, Scala.js
facades, PureScript/ClojureScript FFI), consumer-owned binding tables (Kotlin
`.def`, SWIG `.i`, Rust `bindgen`, cffi/ctypes/c2hs/Zig `@cImport`), and
module-aware Go loading (`go/importer`, `golang.org/x/tools/go/packages`,
`go list`).

- **The loader fork — three options.** (A) Adopt `golang.org/x/tools/go/packages`:
  lowest code, most robust, exactly what the Go team's own generators (`stringer`,
  `gopls`, `mockery`, `moq`) use, and the stdlib `go/importer` docs name it the
  replacement for module-aware loading. Cost: adds `golang.org/x/tools` (plus
  `x/mod`, `x/telemetry`) to the compiler's `go.mod` — a D19/D22 amendment. (B)
  Drive `go list -json -deps` + `go/types` directly: stdlib-only (the compiler's
  `go.mod` stays clean), robust against cgo and build tags because `go list` does
  the module-graph work, but medium-high code (it re-derives a slice of the
  community loader, inheriting `go list`'s version quirks). (C) *chosen for this
  epoch* — stdlib source-mode `go/importer` inside a synthesized module context
  (`go.mod` require+replace→cache, a blank-import anchor): near-zero code, empirically
  sufficient for the pure-Go north star (`lib/pq`), literally preserves D22/D19. Its
  ceiling — legacy-for-general-use `go/build` resolution and a cgo blind spot — is
  named, and (B) is its stdlib-only successor behind the same isolating interface.
  The design defaults to (C) for the first increment and (B) as the robust
  successor, holding (A) as an explicit, reversible escape hatch; the loader
  interface makes the A↔B↔C choice a local one. **This is the genuine fork the RFC
  surfaces for the maintainer's decision** — whether to ship (C) and defer, or start
  at (B) for robustness at higher initial cost.
- **Consumer table: `extern`-as-request vs a separate DSL.** A dedicated table DSL
  (Kotlin `.def` key=value, SWIG typemap language) was considered and rejected: the
  symbols a `kind = "go"` table selects *are* `extern` declarations, and the emit
  target is exactly `extern … @go(…)`, so folding selection, rename, retype, and
  the emit form into one already-specified Aril construct avoids a second surface.
  SWIG's full typemap DSL is the cautionary tail — powerful, famously heavy; the
  design takes the select/rename/retype/annotate quartet the systems agree on and
  resists the DSL.
- **Auto-consume at build vs checked-in generated bindings.** Rust `bindgen`
  supports both; the majority (Kotlin `.klib`, cffi API mode, Zig, c2hs) auto-consume.
  Auto-consume gives reproducibility + zero drift + a loud break on surface change;
  its usual cost (a build-time toolchain dependency, an unreviewable generated blob)
  does not apply to Aril — the Go toolchain is already required, and Aril controls
  diagnostics so a surface change is an Aril-coordinate error (E0126), not a silent
  Go break. Auto-consume is chosen.
- **Library-side inline vs third-party binding ownership.** DefinitelyTyped's
  history shows third-party bindings drift because users, not maintainers, update
  them. The design does not force one model: a Go module author may publish an
  authoritative `kind = "binding"` package, and a consumer may own a `kind = "go"`
  table for a module with no published binding — both validated against the pinned
  Go module identically.
- **One binding per module vs competing bindings.** Scala.js's multiple facades
  per JS library fragment coverage; npm/Kotlin's one-per-library is cleaner. The
  uniqueness rule (E0124) is kept, resolving collisions by canonical Go-module
  identity rather than by binding-package name.

## Transition / compatibility

Additive and staged, per RFC-0008's staged delivery — the increment that reaches
the north star ships before the harder questions bind.

- **Stage 2 first — `kind = "go"`.** A raw Go module + a consumer table, under the
  `database/sql`-with-a-driver north star. This is the increment that introduces
  real Go-binding dependencies and, with them, the binding-uniqueness rule (E0124)
  and the implicit-`binds-go` drift warning (E0125).
- **Stage 3 — `kind = "binding"`.** Strictly sugar over Stage 2 (a published table
  rather than a consumer-owned one); lands last.

A project with no `go`/`binding` dependency is unaffected: the resolver's existing
`kind = "aril"` path and offline build are unchanged. The hand-authored vendored
third-party path (a side manifest + a vendored tree) remains valid and is
superseded incrementally as the fetched-cache `replace` path subsumes it. The
`aril.toml` fields this RFC activates (`kind`, `path`, `binds`, `binds-go`) are
already parsed and schema-validated; no manifest migration is required.

This **elaborates RFC-0008's kinds 2–3** (it does not supersede it — RFC-0008
remains the dependency-system contract, implemented for `kind = "aril"`), and it
**amends D22** for the module-aware loading surface: the stdlib source-mode
importer is driven inside a synthesized module context, with `go list`-driven
loading as its stdlib-only successor and `x/tools/go/packages` an explicit,
un-taken escape hatch. The D19 hermetic-surface guardrail is preserved — a fetched
Go module is declared, version-pinned, lock-verified, and reached only through
`aril get`.

## History

- 2026-07-07 — draft. Grounded in three prior-art passes (published-binding
  formats; consumer-owned binding tables; module-aware Go loading) and an
  empirical loader probe (stdlib source-mode `go/importer` resolves a third-party
  module through a `replace` target given a blank-import anchor, on go1.22).
- 2026-07-07 — draft → accepted. The loader fork is settled toward option C (the
  stdlib source-context loader, behind a swappable interface, with the go-list
  successor and x/tools an un-taken escape hatch). Implementation follows the
  staged delivery (kind=go first, kind=binding as sugar).
- 2026-07-07 — accepted → implemented. Landed over four PRs. **kind=go** — a raw
  Go module bound through a consumer-owned `extern` table, fetched to the cache
  and wired via `require`+`replace` (the go directive floor-driven, `GOTOOLCHAIN=
  local` keeping the build offline). **Module-aware loading** — the option-C
  loader (`LoadModulePackage`): the stdlib source-mode importer inside a
  synthesized module context (a throwaway `go.mod` require+replace→cache + a
  blank-import anchor), reached by `aril import --from`; no `x/tools` dependency
  (D22 preserved). **kind=binding** — a published binding package, its `extern`
  source composed in kind=aril-style plus a `require`+`replace` for the Go module
  it self-declares (`binds`/`binds-go`), fetched alongside it. **Diagnostics** —
  **E0124** (one binding per Go module across the graph, spanning both kinds),
  **E0126** (a table naming an absent Go symbol, validated at `aril get`);
  **E0125** (implicit `binds-go` drift) reserved until multi-binding graphs exist.
  Deferred (carry-forward): blank-import driver registration + a live
  `database/sql`-with-a-driver example (needs a running database, so not a
  hermetic corpus case); the loader's `go list`-driven successor for cgo modules.
