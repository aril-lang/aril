# Project manifest — `aril.toml`

The optional project file that defines a multi-package Aril project's
root and name (RFC-0002). A single-file program or a single directory of
`.aril` files needs **no manifest** — it is one package, resolved against
the stdlib binding registry only. A manifest is required only to span
**more than one user package**.

This is the **authoritative** schema and resolution contract; the prose
mirror is `../docs/language-spec.md`. On disagreement this file wins (D17).

## Schema

`aril.toml` is the **only** v0.x configuration surface — no build flags.
It has four tables — three scalar/list tables plus the repeatable
`[dependencies.<name>]` sub-table:

```toml
[project]
name = "myproj"          # required — the import-path root prefix for
                         # this project's own packages.

[toolchain]
go = "1.22"              # optional — the pinned Go toolchain (matches the
                         # emitted go.mod; lowering-go.md §Output).

[bindings]
extra = []               # optional — extra Go import paths exposed as
                         # bare-ident bindings (the local name is the
                         # last path segment). Two entries sharing a last
                         # segment collide and are rejected at start.
```

### `[dependencies]` — external modules (RFC-0008)

Each external-module dependency is a **named sub-table** whose name is the
dependency's import-path root — the `[project] name` it declares in *its
own* `aril.toml` — so a consumer writes `import <name>/<pkg>`:

```toml
[dependencies.kv]                    # import root `kv` → `import kv/...`
source  = "github.com/alice/aril-kv" # required — the Git/GitHub fetch location (D5)
version = "v1.2.0"                   # required — an exact pin: a tag or commit (D19)
kind    = "aril"                     # optional — aril | binding | go (default aril)

[dependencies.pq]
source  = "github.com/lib/pq"
version = "v1.10.9"
kind    = "go"                       # a raw Go module bound via a local table
path    = "table/pq.aril"            # kind="go" only — the binding table in this project

[dependencies.local]
replace = "../aril-kv"               # optional — a local path overriding source
                                     # (source/version then not required)
```

Fields: **`source`** and **`version`** are required unless **`replace`**
(a local filesystem override) is given. **`kind`** is one of `aril` (a
pure-Aril library, its source compiled in), `binding` (a published
`.go`→`.aril` binding package), or `go` (a raw Go module bound via a
consumer-owned **`path`** binding table); it defaults to `aril`, and
`kind = "go"` requires `path`. A dependency name that duplicates another
`[dependencies.<name>]`, or collides with the project's own `[project]
name`, is rejected. **This is the declared schema; fetching and resolving
these dependencies is later work** — v0.x reads and validates the table,
and the resolver's external-module category is forthcoming.

**Parser.** The reader is a deliberately tiny, closed-schema parser: the
compiler core stays dependency-free (no third-party TOML library — D19),
so only the line shapes above are accepted — `[section]` and
`[dependencies.<name>]` headers, `key = "string"`, `key = ["a", "b"]`
single-line arrays, `#` comments, and blank lines. Anything else (an
unknown section/key, a missing `[project] name`, a malformed value, a
dependency missing a required field) is a manifest error reported at
compiler start.

## Resolution

A package is a directory of `.aril` files (`name-resolution.md` §Scopes —
package scope). When the build target's source tree contains a
`aril.toml` (found by walking up from the entry file/directory to the
filesystem root), each `import P` resolves as:

1. **Local user package.** If `P` equals the manifest `name` or begins
   with `name/`, strip the `name` segment and look up the remainder as a
   directory **relative to the manifest's directory**. Bare `import
   myproj` resolves to the manifest root directory itself. A missing
   directory is **E0117 Unknown import path** (it does *not* fall through
   to stdlib). The package's `.aril` files join the build; its top-level
   names bind under the import's last segment (`import myproj/utils` →
   `utils.…`; the qualified-reference surface and cross-package
   visibility are specified in `name-resolution.md` §Cross-package
   imports).
2. **External module.** If `P`'s root segment names a declared
   `[dependencies.<name>]`, the import resolves into that module's package
   tree: the module root is the dependency's `replace` local path (else its
   fetch-cache location), and the package is the remaining path (`import
   kv/store` → `<kv-root>/store`; bare `import kv` → the module root). A
   `kind = "aril"` module's `.aril` source joins the build and its import is
   stripped from the Go output like a local package; **the module's own
   imports resolve against its `aril.toml`** (its `[project] name` root and
   its own `[dependencies]`), so the import graph — and the acyclic check
   (D20) — span module boundaries. A declared dependency whose module is
   absent (not fetched), lacks an `aril.toml`, or has a not-yet-wired `kind`
   is **E0121** (distinct from E0117, which is an *undeclared* path).
3. **Bundled std module.** A path naming a compiler-bundled Aril module
   (`std/pred` — the contract predicate vocabulary, RFC-0006) resolves to
   the module's **embedded source**, not a directory. Its `.aril` decls
   join the build under a stable virtual path (so `//line` blame and the
   emitted-Go hash stay deterministic), bind by bare name like a merged
   package, and the import is stripped from the Go output. This branch is
   **manifest-independent** — a lone file with no `aril.toml` may
   `import std/pred`.
4. **Stdlib / extra binding.** Otherwise, if the path's head is a known
   stdlib namespace or matches a `[bindings] extra` entry's last segment,
   it resolves through the binding registry (no package directory to
   gather).
5. **Failure.** Neither local, a declared external module, bundled, nor a
   known binding namespace → **E0117 Unknown import path**.

(A bundled `std/*` path is recognised independently of this ordering — it
is an exact-path match that cannot collide with a project name or a
dependency root, so its step number reflects grouping, not strict
precedence.)

Without a `aril.toml`, step 1 is skipped entirely: the program is a
single package resolved against the stdlib registry — the zero-config
path for scripts and quick experiments.

**Acyclic graph (D20).** The import graph — across local packages *and*
external modules — must be acyclic; a cycle (`a` imports `b` imports `a`,
whether in one project or spanning a dependency edge) is **E0116 Cyclic
package import**, rejected before sema runs. Shared code is extracted into
a third package.

**Edge case — `name` collides with a stdlib package** (e.g. `name =
"fmt"`): the local lookup wins for paths under that name; the manifest is
authoritative for the local project. Choosing a non-colliding name is
recommended.

## Fetch — `aril get`

`aril get` is the **only** step that touches the network. It reads the
project's `[dependencies]`, and for each dependency without a `replace`
override it fetches the pinned `source@version` via `git` into a **hermetic
module cache** — `$ARIL_CACHE/<source>@<version>/` (default the per-user
cache dir; `$ARIL_CACHE` overrides). A `source` with no scheme is a
GitHub-style host/path fetched over `https` (D5); a scheme
(`https://`/`file://`/`ssh://`) or a local path is used verbatim. The cache
entry is source-only (git metadata is stripped) and immutable once written.

`aril get` resolves the **transitive** closure (each fetched module's own
`[dependencies]` are fetched recursively). Because v0.x is **exact-pin with
no minimal-version selection**, two modules pinning one dependency to
different versions is a conflict — **E0122**.

`aril build` / `aril run` never fetch: they resolve declared dependencies
against the already-populated cache (a `replace` dependency needs no fetch;
an unfetched one is **E0121**, directing the user to run `aril get`).

## Lockfile — `aril.lock`

`aril get` writes a **committed** `aril.lock` recording, for every module in
the resolved closure, its `source`, declared `version`, the exact commit the
version resolved to at fetch time, and a content hash of the fetched tree —
the reproducibility record (a later build can verify the cache against it).
The format is the same closed-schema line shapes as `aril.toml`
(`[[module]]` blocks of `key = "value"`), so no third-party TOML library
enters the compiler core (D19). It is generated — deterministic, sorted by
module name — and not edited by hand.
