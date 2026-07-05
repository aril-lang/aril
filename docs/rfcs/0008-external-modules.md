# RFC-0008 — External modules: the dependency & build system

| Field | Value |
|---|---|
| Number | 0008 |
| Status | draft |
| Created | 2026-07-05 |
| Supersedes | — |

## Summary

Define how an Aril project **depends on code it does not contain**, in a
**decentralized, git-direct** ecosystem (D5) with no central registry. A module
self-declares its identity in its own `aril.toml`; a consumer declares only a
**requirement** — a `source` (a git path) and a **version constraint**. Three
dependency kinds cover a pure-Aril library, a published `.go`→`.aril` binding
package, and a raw Go module bound through a consumer-owned table.

Version constraints are **caret/tilde/exact** ranges resolved by **minimal
version selection (MVS)** over their lower bounds, with the upper bound applied
as a single-pass compatibility gate. `aril get` fetches the resolved modules
from git into a **hermetic content-addressed cache** and records the exact
resolution in a committed **`aril.lock`** verify-lock (pinning each tag to its
commit). `aril build` / `aril run` resolve **offline** against the cache; the
network is touched only by `aril get`, optionally accelerated by a Go-proxy-style
cache-index that the build never depends on.

Cross-module `.aril` imports join the acyclic import graph (D20), spanning
modules. A module's Aril source compiles into the one emitted Go module — only a
dependency that binds *Go* code introduces a Go-level `require`.

## Motivation

Three durable problems this design answers.

1. **A decentralized ecosystem requires depending on another project's code,
   reproducibly.** D5 makes Aril's package ecosystem decentralized and
   GitHub-hosted — which is meaningful only if a project can *depend on* a module
   that lives in another repository: resolve a compatible version of it against a
   version constraint, fetch it without a central authority, and pin the result so
   every build is identical. A cross-project dependency category in resolution,
   version constraints, a resolution algorithm, a fetch step, and a lockfile are
   what a decentralized ecosystem is made of — the subject of this design.

2. **A dependency system must scale past hand-curation, without a central
   authority.** The alternative to a real dependency system is per-package manual
   plumbing — hand-vendored code, hand-listed manifests, hand-authored bindings.
   And the alternative to *decentralization* is a curated central registry, which
   D5 rejects. The design must therefore resolve dependencies **directly from git**
   (a repo's version tags are its release list), pin them reproducibly, and admit
   an *optional* cache/proxy that only accelerates — never an authority a build
   requires.

3. **The hard bindings need a driver.** `database/sql` — the canonical hard
   binding — is *not stdlib-only*: a working database needs a third-party
   **driver** (`github.com/lib/pq`, …), a raw-Go external dependency. Binding the
   stdlib cannot reach it; it needs a raw-Go dependency (kind 3) plus a
   module-aware `aril import`. The DB-with-driver case is the north star.

## Design

### Overview — modules compose as source, one Go module out

The load-bearing simplification: **an Aril module system is a source-composition
concept, resolved entirely at the Aril layer; a whole program lowers to one Go
module** (`aril-output`). A pure-Aril dependency does not become a separate Go
module — its `.aril` source is compiled into the same emitted Go tree as the
consumer. Only a dependency that *binds Go code* (a binding package or a raw Go
module) introduces a Go-level `require`, riding the hermetic `require`+`replace`
machinery with the replace target pointed at the fetched cache.

```
   aril.toml [dep]  ──►  aril get  ──►  hermetic cache  +  aril.lock
     (constraints)        (network,       ($ARIL_CACHE/         (committed
                           once, opt.      <source>@<version>/)   verify-lock)
                           proxy-cached)         │
   aril build / run  ──── resolve offline (MVS) ─┘
        ├─ aril lib:     dep's .aril source → compiled into the emitted Go tree
        ├─ binding pkg:  published extern bindings (.aril) + a Go require
        └─ raw go+table: aril import (module-aware) over the fetched pkg + a Go require
```

### Ecosystem model — decentralized, git-direct, optional cache-index

A dependency's `source` is a **git repository** (GitHub the default host, D5). Its
**version tags are its release list** — resolution reads them directly
(`git ls-remote`). There is **no central registry**: no curated namespace, no
server-side version metadata as a source of truth, no yank/deprecate authority,
no name reservation.

A **cache-index** — a Go-proxy-style service that mirrors module content and tag
lists — is an admissible *accelerator*, never an authority: it may speed tag
enumeration and content fetches, but a build **works fully without it** (the
`GOPROXY=…,direct` / `off` model). Because the design must not depend on a central
index, registry-centric mechanisms from other ecosystems (central version-listing
APIs, yanking, name reservation, registry-side metadata) are deliberately absent;
their roles are served by the git tags themselves and the committed lockfile.

### Manifests — bidirectional

The relationship between a consumer and a library is two-sided, and each side
declares only what it is authoritative for: **a module self-declares what it
*is*; a consumer declares only what it *requires*.**

#### `[package]` — a module's self-declaration

Every module's `aril.toml` carries a `[package]` section declaring its own
identity and nature. (This supersedes the earlier `[project]` section name — a
module is a *package* whether it is an application root or a library.)

```toml
[package]
name    = "aril-kv"          # required — the import-path root for this module
edition = "2026"             # the project-file/build-system format this manifest targets
kind    = "aril"             # aril | binding  — self-declared (a raw Go module can't; see kind=go)
min-aril = "0.14"            # optional — minimum Aril toolchain this module needs

# kind = "binding" only — the bound Go module this package wraps:
binds    = "github.com/lib/pq"
binds-go = "v1.10.9"         # the bound module version it was generated against (a floor)
```

A consumer that depends on a pure-Aril or published-binding package **reads its
`kind` from the fetched `[package]`** — it does not restate it. This is the
Cargo `[lib] crate-type` / `links`, npm `type`, and per-module `go.mod`
convention: identity and nature are the package's to declare.

#### `[dep.<name>]` — a consumer's requirement

A dependency is a dotted sub-section whose name is the dependency's import-path
root (the `[package] name` it declares in its own manifest), so a consumer writes
`import kv/store` to reach package `store` of the dependency rooted at `kv`.

```toml
[dep.kv]
source  = "github.com/alice/aril-kv"   # required (unless replace) — the git source
version = "^1.3"                        # required (unless replace) — a version constraint

[dep.pq]
source  = "github.com/consumer/aril-pq-binding"
version = "^0.4"
# a kind=go raw-module binding the consumer owns (see The three kinds):
kind    = "go"                          # consumer-side ONLY for a raw Go module
path    = "table/pq.aril"               # kind=go — the consumer-owned binding table

[dep.local]
replace = "../aril-kv"                  # a local filesystem override; not fetched
```

Fields:

- **`source`** *(required unless `replace`)* — the git fetch location; a bare
  host/path is fetched over `https` (D5, GitHub default); an explicit scheme
  (`https://`/`file://`/`ssh://`) or local path is used verbatim.
- **`version`** *(required unless `replace`)* — a version **constraint** (below).
- **`kind`** *(optional)* — for `aril`/`binding` deps it is **omitted** (read from
  the dependency's `[package]`); if present it acts as an **assert-verify guard**
  (a mismatch against the fetched `[package].kind` is a hard error — a
  supply-chain check that you pulled what you expected before its source is
  compiled in). It is **required only for `kind = "go"`** — a raw Go module has no
  `aril.toml` to self-declare, so the consumer must state its nature.
- **`path`** *(`kind = "go"` only)* — the consumer-owned binding table over the raw
  Go module.
- **`binds-go`** *(optional, a binding dep)* — overrides the binding package's
  self-declared bound-Go-module floor (the consumer may bind a newer Go module
  against an existing binding, accepting the ABI-drift risk).
- **`replace`** *(optional, any kind)* — a local filesystem path overriding
  `source`/`version` (dev/vendor escape hatch; not fetched, so needs neither).

#### `[about]` — reserved, free-form, human-only

An optional section reserved for free-form descriptive text (description,
purpose, authorship, links). The toolchain **accepts it and ignores its contents
entirely** — the one deliberate hole in the otherwise closed-schema reader — so
its internal shape stays unspecified and humans read it as-is. Reserving the
*name* now prevents a future clash; a formal descriptive-metadata schema
(license/repository/keywords) lands in `[package]` when a real need arises,
where — with no central registry to hold it — the manifest is its only home.
An `authors` field is admissible for a user's own package (the Aril repository's
own manifests leave it unset, per the no-author-metadata rule).

#### Retained sections

`[toolchain] go` (the pinned Go compiler, now the *root's* choice — see
*Compatibility axes*) and `[bindings] extra` (extra Go import paths surfaced as
bare-ident bindings) are unchanged.

### The three dependency kinds

1. **`aril` — a pure-Aril library.** Its `.aril` source is fetched and compiled
   into the one emitted Go module. No Go `require`. Cross-module visibility is the
   capitalisation-exports rule. This is the D5 decentralized ecosystem in its
   purest form.
2. **`binding` — a published `.go`→`.aril` binding package.** Ships curated
   `extern` bindings (kind-1-like `.aril` source, compiled in) *plus* a
   `[package]` self-declaring the bound Go module + version. That Go module
   becomes a `require`+`replace` (target: the fetched cache).
3. **`go` — a raw Go module + a consumer-owned binding table.** `aril import`
   runs **module-aware** over the fetched Go package to produce `extern` bindings,
   guided by the consumer's `path` table; output is auto-consumed, and the bound
   Go module rides the same `require`+`replace` machinery. This is the
   `database/sql`-driver path. Module-aware loading is the surface D22 already
   carves out for the third-party-plumbing work (`x/tools/go/packages` where the
   stdlib `go/importer` cannot reach a module graph). Having no `aril.toml`, a
   `kind = "go"` dependency declares no Aril floors — it is a **leaf** of the Aril
   MVS graph, and its transitive *Go* dependencies resolve through Go's own
   `require`/`replace`, not Aril resolution.

### Version identifiers — the git-ref convention

Adopt Go's tag grammar (so Aril and Go tooling agree on the same repos), without
Go's heavier machinery:

- **Releases are `vX.Y.Z` semver tags** — the `v` prefix is **required**, valid
  semver, optional `-pre` / `+meta`. A bare `1.2.3` yields a targeted diagnostic
  ("a version tag is `v1.2.3`, not `1.2.3`") rather than a silent miss.
- **An untagged commit is referenced by its full 40-character commit SHA.**
  Pseudo-versions (Go's synthesized `v0.0.0-<ts>-<commit>`) are **not** adopted:
  their only payload is a printable name that *sorts* relative to tags, consumed
  by range ordering — a full SHA already gives reproducibility and identity, so a
  pseudo-version is ceremony until commit-vs-tag ordering is needed (it lands with
  that, if ever).
- **Majors are plain `vN.y.z` tags; one `source` resolves to one `version` per
  build.** Two majors of one dependency do **not** coexist in a build (no
  `/vN`-in-path). Go needs `/vN` because two majors must link in one binary;
  under source-composition the same collision would surface at Go-codegen, so
  side-by-side majors are a future *codegen-namespacing* decision, not a tag
  convention — deferred until v2s appear.
- **One module per repository root** (subdirectory-module tag prefixes deferred).

### Version constraints — caret / tilde / exact

A `version` is a constraint the TS audience already reads fluently:

| Constraint | Range | Note |
|---|---|---|
| `^1.3` | `>=1.3.0, <2.0.0` | caret — floats up to the next breaking axis |
| `^0.4.2` | `>=0.4.2, <0.5.0` | **0.x: the minor is the breaking axis** |
| `^0.0.5` | `>=0.0.5, <0.0.6` | effectively exact |
| `~1.3.2` | `>=1.3.2, <1.4.0` | tilde — patch-only |
| `1.3.*` | `>=1.3.0, <1.4.0` | wildcard |
| `>=1.3, <1.6` | as written | compound |
| `=v1.3.0` / a SHA | exact | the degenerate pin |

The **0.x rule is strict left-most-non-zero** (caret's npm/Cargo semantics): for a
young, 0.x-heavy ecosystem each 0.x minor is treated as potentially breaking, so
`^0.4.2` will not silently adopt `0.5.0`. Exact-pin is not a separate mode — it is
`=v1.3.0`.

### Resolution — MVS over lower bounds + the upper-bound gate

Selection is **minimal version selection (MVS)** applied to the constraints'
**lower bounds**, with the upper bounds applied as a **single-pass compatibility
gate**:

1. Read each constraint's lower bound (`^1.3` → floor `1.3.0`). Each module
   declares its own dependencies' floors in its own `[dep]` — the MVS precondition.
2. The selected version of each module is the **maximum of the floors** required
   for it anywhere in the transitive graph. This is a single graph traversal — no
   SAT, no backtracking — and it is reproducible from the manifests alone.
3. After selection, **assert every declared upper bound holds** for the picked
   version. A genuine conflict (module A requires `<2.0`, the max-of-floors forced
   `2.x`) **fails closed with an explained error** naming both requirers — never a
   silent downgrade or an open-ended search.

MVS resolves the shared-transitive-dependency case that a pure exact-pin scheme
cannot: two dependencies requiring different versions of a shared module resolve
to the **maximum of the two**, automatically, rather than a hard conflict.

Because MVS selects the *minimum that satisfies*, it does not float to the newest
compatible version on its own. **`aril upgrade`** raises floors to the highest tag
within each constraint's window on demand (Go's `-u`, wearing caret clothing), so
default builds stay minimal and reproducible while "give me newest-compatible" is
an explicit, reviewable action.

### Compatibility axes

**The Aril *language* commits to backward-compatibility, so the design carries no
language-versioning machinery.** Edition / `go`-directive padding guards *small,
local, interoperable* breaks — a keyword becoming reserved, a default changing
(Rust's editions: `async`/`await` keywords, disjoint closure captures; Go's
directive, once: per-iteration loop variables in go1.22). It does **not** guard a
*paradigm* break — the pervasive, non-interoperable Python-2→3 kind — where old
and new code cannot coexist; Python's own `from __future__` / `2to3` padding did
not prevent that migration. Aril carries neither mechanism: it commits not to make
small breaks, and treats a paradigm break as a fork-if-ever event no padding would
soften. (Compiling to Go helps — much of what would be a *language* break
elsewhere is a *toolchain / binding-surface* change here, carried by `min-aril`
below.) What genuinely evolves, and therefore needs versioning, is the *toolchain*
and the *project-file / build-system format*:

- **`edition`** — the **project-file / build-system format** a manifest is written
  against (not a language dialect). It is per-manifest, so the toolchain parses
  each module's `aril.toml` under its declared format rules, and manifests of
  different editions interoperate in one build. It lets the format evolve while
  old files keep their old semantics. v0.x defines one edition; the field reserves
  the mechanism.
- **`min-aril`** — a library-side **minimum Aril toolchain** floor. A
  backward-compatible language does not freeze the *toolchain* or the *stdlib-
  binding surface*: a library using a binding added in a later toolchain declares
  that floor. Enforced as a **hard error** (the Go/Cargo lineage, not npm's soft
  warn); with exact resolution there is nothing to *select* on it, so it is a
  build-time check, never a resolution input.
- **Go toolchain** (`[toolchain] go`) — because all Aril modules lower into one Go
  module, there is one Go compiler for the whole program. Libraries *contribute a
  floor*; the **root decides the actual version as the maximum of all floors**
  (mirroring Go's max-of-`go`-directives).
- **Bound Go-module version** (binding kind) — the binding self-declares
  `binds-go` as a **floor** the consumer may override (`binds-go` in the `[dep]`),
  accepting ABI-drift risk. This is the one axis with no clean lineage precedent
  (it is the Cargo `links` / ABI problem).

### Fetch & the cache — `aril get`

`aril get` is the only step that touches the network. It reads the manifest's
`[dep]` constraints, resolves the transitive closure by MVS (enumerating each
`source`'s tags via `git ls-remote`, filtering valid `vX.Y.Z`), fetches each
selected `source@version` via git into a **content-addressed cache**
(`$ARIL_CACHE/<source>@<version>/`, immutable, git-metadata stripped), and writes
`aril.lock`. An optional cache-index may serve tag lists and content faster, but
`aril get` falls back to direct git and never requires it.

### The lockfile — `aril.lock` (verify-lock)

`aril.lock` is a **committed verify-lock**, not a select-lock. Under MVS the
manifests alone determine the selection, so the lockfile does **not** pick — it
**verifies and freezes the git reality**: per resolved module, its `source`, the
resolved `version`, the **exact commit the version's tag pointed at**, and a
content hash. Pinning tag→commit is *more* valuable than a hash-only lock because
git tags are mutable — a re-tag or force-push cannot silently change the bytes a
build compiles. The format is the closed-schema line shapes the manifest reader
uses (no third-party TOML library, D19), generated and sorted.

### Offline builds

`aril build` / `aril run` never fetch. They re-derive the MVS selection from the
manifests, verify it against `aril.lock` and the cache, and compile — offline. A
`replace` dependency needs no fetch; a declared dependency absent from the cache
directs the user to run `aril get`.

### Diagnostics (Aril-coordinate, D10)

- **E0117** — an import matching no local package, builtin module, or declared
  dependency (an *undeclared* path).
- **E0121** — a declared dependency that is not resolvable (absent from the cache,
  no `aril.toml`, or a not-yet-wired kind).
- **E0122** — a version-compatibility conflict the upper-bound gate rejects (the
  max-of-floors violates a declared ceiling), naming both requirers.
- **E0123** — `aril.lock` stale or a cache hash mismatch (re-run `aril get`).
- Further Aril-coordinate diagnostics — a bare tag missing its `v`, a manifest
  `edition` the toolchain does not support, a `min-aril` above the running
  toolchain, and a `[dep].kind` guard disagreeing with the fetched
  `[package].kind` — are allocated in `diagnostics.md` at implementation.

## Alternatives considered

Grounded in prior-art passes over Cargo, Go modules, npm/pnpm/Yarn, Deno, Nix,
and Dart/uv.

- **Exact-pin only vs. version ranges + resolution.** A pure exact-pin manifest
  needs no resolver, but makes two dependencies pinning different versions of a
  shared transitive module a hard conflict with no recourse — and no ecosystem
  ships without ranges. Ranges + a resolver are adopted; the exact pin survives as
  the degenerate `=v1.3.0`.
- **MVS vs. newest-compatible backtracking (Cargo, npm) vs. PubGrub (Dart, uv).**
  Newest-compatible picks the *highest* satisfying version and needs a
  select-freeze lockfile plus backtracking; PubGrub adds excellent conflict
  explanations but keeps the backtracking + select-lock. **MVS** is chosen: it is
  Go-native, needs no SAT and no select-lock (reproducible from manifests alone),
  and its precondition — each module declaring its own dependencies' floors — is
  exactly the bidirectional-manifest shape adopted here. Under git-direct (no
  registry), MVS's single-pass "lowest tag ≥ floor" query is cheaper than a
  backtracker re-listing a repo's tags. Its cost — no newest-by-default — is paid
  by the explicit `aril upgrade`. See Russ Cox, *Minimal Version Selection*
  (research.swtch.com/vgo-mvs) and the *PubGrub* writeup (nex3.medium.com).
- **Caret/tilde syntax lowered to MVS floors vs. Go-minimal bare minimums.** Go
  omits range syntax entirely (bare minimums + major-in-path). The TS audience
  reads `^1.3` fluently, so the caret/tilde surface is kept — but resolved by its
  *lower bound* through MVS, with the upper bound as a gate. This grants the
  familiar spelling over the simpler engine, and keeps a real upper-bound check
  Go's bare-minimum model lacks.
- **Library-side vs. consumer-declared `kind`.** No mainstream system makes a
  consumer re-assert what a dependency *is* (Cargo `crate-type`/`links`, npm
  `type`, Go's per-module `go.mod`). Kind is self-declared; the consumer restates
  it only for `kind = "go"`, where there is no `aril.toml` to read — the forced
  asymmetry, not a wart.
- **Central registry (npm, crates.io) vs. git-direct + optional proxy (Go).** A
  curated registry conflicts with D5's decentralization and imports mechanisms
  (yank, name reservation, server-side metadata) with no home in a git-direct
  world. The Go model — tags as the release list, an *optional* proxy that only
  accelerates — is adopted.
- **One language-version axis (Go `go 1.x`) vs. two (Cargo `edition` +
  `rust-version`) vs. none.** With a backward-compatible language, a *language*
  edition has no purpose. Two axes remain, repurposed to what actually evolves: a
  **build-system-format `edition`** and a **toolchain `min-aril` floor** — distinct
  granularities (coarse format epoch vs. fine toolchain floor), both library-side.
- **`/vN` major-in-path (Go SIV) vs. one version per source.** Go's semantic
  import versioning lets two majors link in one binary because they are distinct
  import paths. Under source-composition the collision moves to Go-codegen, so
  side-by-side majors are deferred as a codegen-namespacing decision; a build
  resolves one `source` to one `version`.
- **Pseudo-versions vs. raw commit SHA.** A pseudo-version's sortability only pays
  off under range ordering across tagged and untagged points; a full SHA already
  gives identity and reproducibility, so untagged commits use the SHA, and Go's
  pseudo-version format is adopted only if commit-ordering is ever needed.
- **Global content-addressed cache (pnpm store, Go module cache, Nix) vs.
  per-project vendored copies (npm `node_modules`).** Per-project copies are the
  `node_modules` bloat the TS audience is fleeing. The dependency cache is a single
  global content-addressed store (`$ARIL_CACHE`); dependencies are never copied
  per-project. (Because Aril compiles through Go, whose build/module caches are
  already global, a project's own emitted output stays small — the artifact-layout
  choice is RFC-0009.)
- **Select-lock vs. verify-lock.** Under newest-compatible the lockfile must both
  pick and verify; under MVS it only verifies. The verify-lock is chosen, extended
  to pin tag→commit (guarding against mutable git tags) — strictly more than a
  hash-only lock.

## Transition / compatibility

Additive to a project with no `[dep]` — resolution's external category is skipped,
`aril get` is a no-op, no lockfile is needed. The `[project]` section is renamed to
`[package]` and gains self-declaration fields; a consumer's `kind` for aril/binding
deps becomes optional (a guard) rather than required. Because the pre-revision
surface has a single consumer (the `greeter` example, via `replace` with no
version), migration is a one-file edit at implementation. The hand-vendored FFI
path (`std/bindings.json` + `std/vendor`) remains valid and is superseded
incrementally.

At the decision level this **realizes D5** (a decentralized ecosystem with a real
build system) and **restates the D19 guardrail** (amended by RFC-0005 to admit
pinned hermetic third-party deps) for the fetched-cache mechanism — declared,
version-pinned, lock-verified, `get`-only network. Two peer concerns are
deliberately *not* folded in: the **build-artifact layout** (`aril-out/`, RFC-0009)
and a **user-facing test surface** (its own RFC), each with its own prior-art pass.

## History

- 2026-07-05 — first draft: the dependency & build system with exact-pin versions,
  a consumer-declared `kind`, and no versioning/edition or resolution model.
- 2026-07-05 — substantially revised after four prior-art research passes
  (build-artifact layout; library-side manifest + version compatibility;
  version→git-ref mapping; version ranges + resolution algorithms) and a design
  review. Changes: the bidirectional `[package]`/`[dep]` manifest split (kind
  self-declared library-side; `[about]` reserved); version **ranges** (caret/tilde)
  resolved by **MVS** over lower bounds with an upper-bound gate; the `vX.Y.Z`
  git-ref convention (SHA for untagged commits, one version per source);
  compatibility axes reframed for a backward-compatible language (a build-system-
  format `edition` + a `min-aril` toolchain floor, not a language dialect); the
  git-direct + optional-proxy ecosystem model; and a tag→commit verify-lock.
  Build-artifact layout split to RFC-0009; a test surface deferred to its own RFC.
