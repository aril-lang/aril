# RFC-0008 — External modules: the dependency & build system

| Field | Value |
|---|---|
| Number | 0008 |
| Status | draft |
| Created | 2026-07-05 |
| Supersedes | — |

## Summary

Define how an Aril project **depends on code it does not contain** — another
project's Aril library, a published Go-binding package, or a raw Go module
plus a binding table. A new `[dep]` section in `aril.toml` declares
each dependency by **source** (a Git/GitHub path, D5), a **pinned version**,
and a **kind** (one of three). A new step, `aril get`, fetches the pinned
dependencies into a **hermetic content-addressed cache** and records the exact
resolved set in a committed **`aril.lock`**. `aril build` / `aril run` resolve
imports against that cache **offline** — the network is touched only by
`aril get`. Cross-module `.aril` imports join the existing acyclic import graph
(D20), now spanning modules rather than only the packages of one project.

This is the dependency half of the **cold-start problem** — binding the stdlib
gives a usable language but not an ecosystem, and the ecosystem starts empty. The
binding generator (`aril import`, RFC-0005) already exists but is stdlib-only;
this RFC gives an Aril project the ability to *depend on* an external module, and
makes `aril import` module-aware so it can bind a fetched non-stdlib Go package.

Single-file scripts and single-project builds are unaffected — a project with
no `[dep]` behaves exactly as today.

## Motivation

Three forces, in order.

1. **There is no ecosystem without this.** D5 declares a *decentralized*,
   GitHub-hosted Aril package ecosystem. Today nothing implements it: the
   resolver (`cmd/aril/resolve.go`) classifies an import as a compiler-bundled
   `std/*` module, a **local** package under the single project's `[project]
   name`, a builtin Go/runtime module, or a `[bindings] extra` Go path —
   anything else is `E0117 unknown import path`. There is no category for a
   package that lives in *another* project. RFC-0002 explicitly parked this:
   *"Versioned dependencies. Out of scope; pre-alpha has no package manager."*
   That parking ends here.

2. **The third-party FFI path is hand-plumbed and does not scale.** RFC-0005
   shipped the generator and a hermetic third-party path, but the mechanics are
   manual: the Go module is **hand-vendored** under `std/vendor/`, **hand-listed**
   in `std/bindings.json`, and the `extern` bindings are **hand-authored**. The
   go.mod `require`+`replace` is derived by string-scanning the emitted Go
   (`cmd/aril/thirdparty.go`). This is exactly the "vendored, hermetic,
   hand-curated" path the roadmap says must generalize into a *fetched, resolved,
   reproducible* dependency system. RFC-0005 flagged the mechanism as an open
   question (Open-Q5: *replace-to-vendored vs a committed module cache vs an
   `aril.toml`-declared dependency set*); this RFC answers it: **all three,
   layered** — a manifest-declared set, resolved into a committed-lock + cache,
   with `replace` as the local-override escape hatch.

3. **The hard bindings need a driver.** `database/sql` — the canonical hard
   binding — is *not stdlib-only*: a working database needs a
   third-party **driver** (`github.com/lib/pq`, …), a raw-Go external dependency.
   It cannot be reached by binding the stdlib; it requires exactly the capability
   this RFC adds (a raw-Go dependency, kind 3) plus a module-aware `aril import`.
   The DB-with-driver case is this epoch's north star.

## Design

### Overview — modules resolve at the Aril layer, one Go module out

The load-bearing simplification: **Aril's module system is a *source-composition*
concept, resolved entirely at the Aril layer; the whole program still lowers to
one Go module** (`aril-output`, as today). An Aril `.aril`-library dependency
does not become a separate Go module — its Aril source is compiled into the same
emitted Go tree as the consumer, as an additional package subtree. Only a
dependency that *binds Go code* (kinds 2–3) introduces a Go-level `require`, and
that rides the existing `thirdparty.go` machinery, generalized from
hand-vendored paths to fetched-cache paths.

```
   aril.toml [dep]  ──►  aril get  ──►  hermetic cache  +  aril.lock
        (declared, pinned)         (network,        ($ARIL_CACHE/          (committed,
                                    once)            <src>@<ver>/)          reproducible)
                                                          │
   aril build / run  ──── resolve imports (offline) ──────┘
        │
        ├─ kind 1 (.aril lib):    dep's .aril source → compiled into the emitted Go tree
        ├─ kind 2 (binding pkg):  pre-generated extern bindings + a Go require (fetched replace)
        └─ kind 3 (raw .go+table): aril import (module-aware) over the fetched pkg → bindings + require
```

### The manifest — `[dep]`

A dependency is a **dotted sub-section** whose name is the dependency's
**import-path root** — the `[project] name` the dependency declares in *its own*
`aril.toml`. This matches the existing `import <root>/<pkg>` form (RFC-0002) and
the `[bindings] extra` last-segment convention: a consumer writes `import kv/store`
to reach package `store` of the dependency rooted at `kv`.

```toml
[project]
name = "myapp"

[dep.kv]                          # import root `kv` → `import kv/...`
source  = "github.com/alice/aril-kv"       # where to fetch (Git/GitHub, D5)
version = "v1.2.0"                          # a pinned tag or commit (hermetic, D19)
kind    = "aril"                            # aril | binding | go   (default: aril)

[dep.pq]
source  = "github.com/lib/pq"
version = "v1.10.9"
kind    = "go"                             # raw Go module + binding table
path    = "table/pq.aril"                  # kind=go: the co-located binding table (in *this* project)
```

Fields per dependency:

- **`source`** *(required unless `replace`)* — the fetch location: a Git path,
  GitHub the default host (D5). The transport is `git`; a tag/commit at `source`
  is the unit of fetch. (A future registry/proxy is an optional accelerator, not
  required — D5 is decentralized-first.)
- **`version`** *(required unless `replace`)* — an **exact pin**: a Git tag
  (`v1.2.0`) or a commit SHA. v0.x does **not** do SemVer range-solving (see
  *Version resolution*).
- **`kind`** *(optional, default `aril`)* — one of:
  - **`aril`** — a pure-Aril library (kind 1). Its `.aril` source compiles into
    the build.
  - **`binding`** — a *published* `.go`→`.aril` binding package (kind 2): ships
    curated `extern` bindings plus a manifest naming the bound Go module+version.
  - **`go`** — a raw Go module (kind 3): bound via a **local binding table**
    (`path`) — generic-bindgen applied per-dependency.
- **`path`** *(kind `go` only)* — the binding table in *this* project that tells
  Aril how to surface the raw Go module (the per-module analogue of the curated
  `internal/binding.Manifest`). For kind `binding` the table is published *by the
  dependency*; for kind `go` the consumer owns it.
- **`replace`** *(optional, any kind)* — a local filesystem path that overrides
  `source`/`version` for this build (the `go.mod replace` analog). The
  dev/offline/vendor escape hatch; a `replace`d dependency is not fetched, so it
  needs neither `source` nor `version`.

The closed-schema reader (`cmd/aril/manifest.go`) grows one capability: a
**dotted section header** `[dep.<name>]`. The line-shapes stay
`key = "value"`; no inline-table parser is needed. The `<name>` binds the same
last-segment collision rule `[bindings] extra` already enforces.

### The lockfile — `aril.lock`

`aril get` resolves the full transitive dependency set and writes a **committed**
`aril.lock` pinning, for every module (direct and transitive): its `source`, the
**exact resolved version** (a tag resolves to the commit it named at fetch time),
and a **content hash** of the fetched tree. The lock is the reproducibility +
hermeticity contract (D19): `aril build` reads the lock, verifies the cache
matches the hashes, and **never touches the network**. A lock that is missing or
stale relative to `aril.toml` is a clear diagnostic directing the user to run
`aril get`, never a silent network fetch at build time.

The lockfile format is a small committed TOML/line table (same closed-schema
reader family); its exact shape is an implementation detail settled in PR4.

### Fetch & the hermetic cache — `aril get`

`aril get`:

1. Reads `[dep]`, resolves the transitive closure (each fetched
   module's own `aril.toml` `[dep]` are read recursively).
2. Fetches each pinned `source@version` via `git` into a **content-addressed
   cache** — `$ARIL_CACHE/<source>@<version>/` (default `~/.cache/aril/`,
   overridable by `$ARIL_CACHE`; `std/vendor` is the special-cased in-tree cache
   for the compiler's own fixtures). A cache entry is immutable once written.
3. Writes/updates `aril.lock`.

`aril get` is the **only** network step. A CI or offline build populates the
cache once (or vendors via `replace`), commits the lock, and thereafter builds
hermetically. This is the D19 guardrail restated for fetched code: an external
dependency enters generated output **only** when *declared* in `[dep]`,
*version-pinned*, and *lock-verified against the cache* — never an ambient or
transitive network pull. `aril get` fetches; `aril build`/`run` are offline.

### Version resolution — exact-pin first

v0.x resolution is deliberately minimal: **exact pins, no range-solving.** Each
declared `version` is a tag or commit; transitive dependencies carry their own
exact pins. If two modules in the closure require *different* versions of the
same module, that is an **error** in v0.x (`E0122` dependency version conflict),
not an automatic minimal-version-selection. This keeps the first cut tractable,
hermetic, and honest; a SemVer/MVS solver is a later refinement, added only when
the ecosystem is large enough to need it. The single-version
rule mirrors Go's "one major version, one module" at a coarser grain.

### Resolution — a new `importExternal` category

`classifyImport` (`resolve.go`) gains a category between local-user and unknown:
after the `[project] name` local check fails, the head segment is matched against
the declared `[dep]` roots. A match resolves to the dependency's module
directory **in the cache** (per the lock), and cross-module `.aril` resolution
proceeds exactly as cross-package does today — the DFS in `resolvePackages`
walks into the external module's package tree, and the acyclic-graph check (D20,
`E0116`) now spans module boundaries. An import whose head matches no local
package, no builtin module, and no declared dependency is still `E0117`; a
declared dependency that is not present in the cache/lock is a new,
directed-at-`aril get` diagnostic (`E0121`), distinct from `E0117`.

Spanning modules broadens the last-segment binding surface (RFC-0002: a package
binds under its final path segment). Two dependencies exposing same-named
sub-packages (`kv/store` and `db/store` both bind `store`), or a dependency
package colliding with a local one, is a new collision case RFC-0002 has no
aliasing form for yet. v0.x makes a collision an error at resolution rather than
silently shadowing; an `import … as` alias is the eventual escape hatch (RFC-0002
parked it), promoted when the first real conflict lands.

### The three kinds at build time

- **Kind 1 — `.aril` library.** The dependency's `.aril` source is pulled into
  the emitted Go tree as additional package subtrees under the one `aril-output`
  Go module. No Go `require`. Cross-module visibility is the existing
  capitalisation-exports rule (RFC-0002 §Visibility). This is the D5
  decentralized ecosystem in its purest form.
- **Kind 2 — published binding package.** Ships curated `extern` bindings (kind-1
  `.aril` source, compiled in) **plus** a manifest naming the bound Go module +
  version. That Go module becomes a `require`+`replace` in the emitted go.mod —
  the existing `thirdparty.go` path, generalized so the `replace` target is the
  fetched-cache path and the `require` is read from the dependency's manifest
  rather than hand-listed in `std/bindings.json`.
- **Kind 3 — raw Go + local binding table.** `aril import` runs **module-aware**
  (below) over the fetched Go package to produce `extern` bindings, guided by the
  consumer-owned binding table (`path`). Output is auto-consumed into the build
  (not stdout-only), and the bound Go module rides the same `require`+`replace`
  machinery as kind 2. This is the `database/sql` driver path.

### Module-aware generic-bindgen

`aril import` today uses `go/importer` source mode (D22), which loads a package
from GOROOT/GOPATH source and is **not module-graph-aware** — so it reliably
binds only the stdlib (`lang-spec/ffi.md` flags module-aware loading as a
follow-up). This RFC provides what that follow-up needs: the fetched dependency
lives in the cache as real module source, so a **module-aware load against the
cache** (rooted at the fetched module, with its `go.mod` resolved through the
lock) can introspect a non-stdlib package's types. The loader change is scoped
to `internal/bindgen`; the D22 "no `x/tools/go/packages` in the compiler core"
constraint is re-examined here (a module-aware load may need more than
`go/importer`) — resolved in PR6, and a candidate D22 amendment if the stdlib
importer proves insufficient for module graphs.

### Diagnostics (new codes, `.aril`-coordinate, D10)

- **`E0121`** — declared dependency not fetched (run `aril get`). Names the
  dependency and the missing cache entry.
- **`E0122`** — dependency version conflict (two pins for one module) in the
  transitive closure. Names both requirers and both versions.
- **`E0123`** — lockfile out of date / hash mismatch (the cache does not match
  `aril.lock`; re-run `aril get`). Guards against a tampered or partial cache.
- `E0116` (cyclic import) and `E0117` (unknown import) extend across module
  boundaries unchanged. A manifest-level error (bad `[dep]` shape,
  unknown `kind`, colliding roots) surfaces at manifest-parse time in
  `aril.toml` coordinates, mirroring the existing manifest errors.

## Alternatives considered

- **A SemVer/MVS solver from day one** (Go-style minimal version selection).
  Rejected for v0.x: the ecosystem is empty, so there is nothing to solve; a
  solver is complexity with no payload today. Exact-pin + conflict-is-an-error is
  honest and hermetic, and the lockfile means the upgrade to MVS later is
  behaviour-adding, not breaking.
- **A central registry/proxy as the primary source** (npm/crates.io/GOPROXY
  shape). Rejected as the *primary* path: D5 is decentralized-first (Git/GitHub,
  like Go modules pre-proxy). A proxy is a legitimate *optional accelerator*
  layered on top later, not the ground truth.
- **Each Aril module → its own Go module** (mirror Aril modules onto Go modules
  1:1). Rejected: it forces a multi-`go.mod` build tree, `init()` glue, and Go's
  module resolver into a problem Aril already solves at the source layer. Aril
  modules compose as *source*; one Go module out keeps the emitted tree and the
  `//line` blame model unchanged.
- **Extend `std/bindings.json` into the dependency system** (grow the existing
  hand-list). Rejected: it is a compiler-internal fixture list keyed on emitted-Go
  string-scan, not a per-project declared surface. The project manifest
  (`aril.toml`) is the right home for a *project's* dependencies;
  `std/bindings.json` stays the compiler's own in-tree vendor cache.
- **Fetch at build time (no separate `aril get`).** Rejected: it makes every
  build network-dependent and non-hermetic, breaking D19 and CI reproducibility.
  The `get`/`build` split (fetch-once, build-offline) is the Go/Cargo/npm
  consensus and the only one compatible with the hermeticity guardrail.

## Transition / compatibility

Strictly additive. A project with no `[dep]` — every corpus example
today — builds unchanged (the resolver's new category is skipped, `aril get` is a
no-op, no lockfile is needed). `aril.toml`, `aril.lock`, and the cache are all
opt-in, activated only when a dependency is declared. No existing program
changes; no user migration. The existing hand-vendored third-party FFI path
(`std/bindings.json` + `std/vendor`) keeps working and is superseded
*incrementally* — each hand-vendored dep can move to a `[dep]`
declaration behaviour-preservingly (the `build_ok` ratchet guards this), until
the hand-list is empty.

At the decision level this **realizes D5** (previously aspirational) and
**restates the D19 guardrail** (already amended by RFC-0005 to admit pinned
hermetic third-party deps) for the fetched-cache mechanism — no new decision
conflict; the guardrails (declared, pinned, lock-verified, `get`-only network)
are exactly D19's intent.

## History

- 2026-07-05 — drafted. Opens the EXTERNAL-MODULES epoch as its contract, on a
  live reconnaissance of the existing manifest/resolver/FFI/bindgen machinery.
  Resolves RFC-0002's parked "versioned dependencies" and RFC-0005's Open-Q5
  (hermeticity mechanism). Implementation is the epoch's PR2–PR6.
