# RFC-0009 — Build-artifact layout

| Field | Value |
|---|---|
| Number | 0009 |
| Status | implemented |
| Created | 2026-07-05 |
| Supersedes | — |

## Summary

Define **where Aril writes build artifacts** and **where it caches
dependencies**. A per-project **`aril-out/`** directory (visible, git-ignored)
holds the final binary (`aril-out/bin/<name>`) and the persisted lowered Go
(`aril-out/gen/`). The dependency cache is the **global `$ARIL_CACHE`**
(RFC-0008) — shared across projects, never copied per-project.
The output location is configured, in precedence order, by `--out-dir`, the
`ARIL_OUT` environment variable, a `[build] out-dir` manifest key, and the
default `./aril-out`.

The result: one answer to "where are my artifacts?", no loose binaries dropped
in the source tree, and persisted intermediate Go so the Go toolchain's own
build cache makes rebuilds incremental.

## Motivation

Three durable problems a compiler toolchain must answer.

1. **Discoverability — one place for artifacts.** A build produces a native
   binary and intermediate output; a developer needs a single, obvious location
   to find, inspect, and clean them. A scattered or cwd-relative answer forces
   the developer to know each command's output convention.

2. **No litter in the source tree.** Emitting a binary loose in the working
   directory (the Go default) leaves an untracked artifact beside the source —
   the sloppiness a `dist/`-style convention exists to prevent, and a hazard for
   a blanket `git add`. Artifacts belong in one ignorable directory, not
   scattered among sources.

3. **Incremental rebuilds.** Aril lowers to Go and invokes the Go toolchain.
   Lowering to a fresh throwaway directory each build defeats Go's own
   content-addressed build cache — every rebuild recompiles from scratch. A
   *persisted* intermediate directory lets an unchanged lowering hit that cache.

## Design

### The per-project output directory — `aril-out/`

A build writes into a single directory at the project root:

```
myproj/
  aril.toml
  main.aril
  aril-out/            # git-ignored (auto)
    bin/myproj         # the final native binary
    gen/               # the lowered Go module (the IR), persisted
    .gitignore         # "*" — auto-generated
```

- **`aril-out/bin/<name>`** — the final binary, the default target of
  `aril build`. An explicit `-o <path>` sets the binary path outright (it may sit
  outside `aril-out/`); the directory knobs below relocate only the housing
  directory, not the `-o` target.
- **`aril-out/gen/`** — the lowered Go module (whose go.mod module path is the
  distinct constant `aril-output` — the *directory* `aril-out/` and the *module
  name* `aril-output` are not the same string). Aril lowers here and **keeps it**
  between builds rather than lowering to a throwaway temporary directory.
- **`aril-out/.gitignore`** — auto-generated, containing `*`, so every artifact
  is ignored even if the developer forgets to add `/aril-out` to the project's
  own `.gitignore` (the discipline Cargo applies to `target/`).

`aril-out/` is visible (not a hidden `.aril/`): the primary goal is
discoverability for a TypeScript-refugee audience that already reads a top-level
`dist/`, and a visible directory answers "where is my binary?" at a glance.

### Persisted lowered Go — the incremental-build payoff

The lowered Go in `aril-out/gen/` is retained across builds. Go's build cache
(`$GOCACHE`) keys the program's own package on its absolute source path, so
lowering to a fresh temporary directory each build misses the cache and
recompiles that package; a persisted `gen/` holds the path stable, so an
unchanged lowering hits the cache and the rebuild is incremental. Persistence is
justified by that incremental-build payoff **alone**. Go remains an intermediate
representation the developer never works in (D1/D16) — that the IR happens to sit
on disk is an incidental consequence, neither a goal nor a supported interface;
the debugging path is `//line` back to `.aril` coordinates (D8), not reading the
generated Go.

Persistence obliges Aril to keep `gen/` **in sync** with the sources. When a
`.aril` file is renamed or removed, its previously-emitted `.go` must be deleted —
otherwise `go build` compiles the stale file and the developer sees phantom errors
from a source that no longer exists (the most likely first-week bug of a naive
persistent cache). Aril records the set of files it emits (a manifest under
`aril-out/`) and removes orphans on each lowering; wiping `gen/` wholesale per
build would be simpler but would discard the very incremental-cache win this
section exists for.

### The dependency cache — global, coordinate-addressed

Fetched dependency modules live in the **global** `$ARIL_CACHE` (RFC-0008) —
coordinate-addressed (`<source>@<version>`) and content-verified against the lock,
shared across every project on the machine and **never copied into `aril-out/`**.
This is the pnpm-store / Go-module-cache / Nix-store role — the relief from the
per-project duplication of `node_modules`. If per-project visibility of
dependencies is ever wanted, the answer is to *link* (pnpm-style), never to copy.

### The build cache — delegated to Go

Aril compiles *through* Go, whose build cache (`$GOCACHE`) is already global and
content-addressed. Aril therefore delegates build caching to it and keeps
`$ARIL_CACHE` as the *module* cache only. A distinct Aril-level build cache is
unnecessary until Aril grows its own pre-Go caching (of type-checking or
lowering); the naming leaves room for it (`$ARIL_CACHE` names the module cache,
not "the cache").

### Configuration

The output directory is resolved by the first of, in order:

1. `--out-dir <path>` — a per-invocation flag;
2. `ARIL_OUT` — an environment variable;
3. `out-dir` under a `[build]` table in `aril.toml`;
4. the default `./aril-out` (relative to the project root).

This mirrors Cargo's proven precedence (`--target-dir` › `CARGO_TARGET_DIR` ›
`build.target-dir` › default). Two environment variables, one per concern, name
the two locations a developer may need to relocate: **`ARIL_OUT`** (build output)
and **`ARIL_CACHE`** (the module cache) — the "one memorable variable per
concern" discipline of `CARGO_TARGET_DIR` / `GOCACHE` / `DENO_DIR`.

**A shared output directory is namespaced per project.** When `ARIL_OUT` or
`--out-dir` points several projects at *one* directory — the example-corpus runner
is the motivating case — the layout gains a project segment,
`<out>/<project-id>/{bin,gen}`, where `<project-id>` is the project's `[project]
name` plus a short hash of its root path. Without it, co-located projects would
overwrite each other's `gen/` and collide in one `bin/` on a name clash. Under the
default `./aril-out` (one project, one directory) the segment is omitted. Cargo
keys a shared `CARGO_TARGET_DIR` the same way.

### Concurrent builds

A build takes an **exclusive lock** on its output directory (`aril-out/`, or the
per-project segment under a shared out-dir) for the span of lowering plus
`go build`. Two concurrent `aril build`/`run` in one project — a parallel corpus
run is exactly the in-house case — thus serialize on `gen/` rather than corrupting
each other's intermediate Go; the second waits for the lock. Cargo locks `target/`
for the same reason.

### Cleaning and reserved slots

`aril clean` removes `aril-out/` — the whole tree, or `gen/`/`bin/` selectively —
the counterpart to a persisted layout.

Cross-compilation is deferred, but the layout **reserves its slot** the way it
reserves one for build profiles: per-`GOOS`/`GOARCH` outputs land under a
target-triple segment (`aril-out/bin/<target>/<name>`, `gen/<target>/`), so adding
cross-compilation later needs no relayout.

## Alternatives considered

Grounded in a prior-art pass over Cargo, Go, npm/pnpm/Yarn, Bazel, Deno, Nix, and
Zig.

- **A per-project build dir (Cargo `target/`) vs. no project dir (Go).** Go's
  no-directory model drops the binary in the working directory (`go build`;
  `go install` targets `$GOBIN` instead), which is the source-tree-litter
  problem, and it has no obvious `.gitignore` target.
  Cargo's `target/` gives one discoverable, ignorable home. Aril takes `target/`'s
  discoverability **without** its notorious disk bloat: Cargo's `target/` is heavy
  mostly with *per-project compiled dependency objects*, but Aril compiles through
  Go, whose `$GOCACHE`/`$GOMODCACHE` are already global — so `aril-out/` holds only
  *this* project's lowered Go and its binary, and stays small.
- **Visible `aril-out/` vs. hidden `.aril/`.** Hidden reduces `ls` clutter and
  signals "tooling"; visible maximises discoverability and matches the `dist/`
  habit of the target audience. Visible is chosen for discoverability.
- **`debug`/`release` profile sub-directories (Cargo, .NET).** Aril has no
  build-profile dichotomy — contracts on/off, `-race`, and vendored-vs-inline
  runtime are profile-*like* axes but not a fixed pair. A profile sub-directory
  (`aril-out/<profile>/`) is the idiomatic slot should a profile distinction ever
  land; it is deferred, and adding it later costs no redesign.
- **A single global module cache (pnpm store, Go module cache, Nix) vs.
  per-project vendored copies (npm `node_modules`).** Per-project copies are the
  `node_modules` duplication the TypeScript audience is fleeing. A single global
  cache (`$ARIL_CACHE`, coordinate-addressed and content-verified — RFC-0008) is
  chosen; dependencies are never copied per-project.
- **Persisting the lowered Go vs. a throwaway temp directory.** Lowering to an OS
  temp directory and deleting it keeps generated Go fully out of sight but forces
  a full recompile every build (a fresh path defeats Go's cache). Persisting to
  `aril-out/gen/` keeps the source path stable and so unlocks Go's incremental
  `$GOCACHE`, at the cost of a cleanup surface. Persisting is chosen for that
  build-speed payoff; Go stays an IR the developer never works in (D1/D16), and
  its on-disk presence is incidental, not an interface.
- **One cache variable vs. splitting module and build caches now.** Go separates
  `$GOCACHE` (build) from `$GOMODCACHE` (module). Aril rides Go's `$GOCACHE` for
  build caching, so `$ARIL_CACHE` need only be the module cache — one variable. A
  second is introduced only if Aril grows its own pre-Go build cache.
- **The name `aril-out` vs. `target` (Cargo) vs. `build`/`dist`.** `target`
  carries Rust baggage ("`target/` is huge") and is less recognisable to a
  TypeScript audience; `build`/`dist` are overloaded by JS tooling. `aril-out`
  is self-descriptive and namespaced to the toolchain.

Sources: the Cargo Book (Build Cache; Environment Variables; Configuration), the
`cmd/go` reference (`$GOCACHE`, `$GOMODCACHE`, `$GOBIN`), pnpm (symlinked
`node_modules`; store), and the Bazel output-directory layout.

## Transition / compatibility

Additive. `aril build -o <path>` continues to override the output location; only
the *default* changes — from a loose `./<basename>` to `aril-out/bin/<name>` —
which removes the source-tree litter. `aril run` writes its intermediate Go to
`aril-out/gen/` rather than a throwaway temporary directory. No manifest change is
required (the `[build] out-dir` key is optional).

Tooling that builds many projects in one tree — the example-corpus runner — sets
`ARIL_OUT` to a scratch directory so the persisted default does not accrete an
untracked `aril-out/` under each example (litter in the source tree; the binary
is already redirected by `-o`, but persisted `gen/` is independent of it). This
is the one integration point the persisted default introduces.

This RFC is the build-artifact peer of RFC-0008 (the dependency & build system);
RFC-0008 owns `$ARIL_CACHE` and the dependency model, this one owns the
per-project output layout. A user-facing test surface (`aril test`) is a separate
concern with its own RFC.

## History

- 2026-07-05 — drafted, as the build-artifact-layout peer of RFC-0008, grounded in
  a prior-art pass over Cargo `target/` + `CARGO_HOME`, Go's `$GOCACHE` /
  `$GOMODCACHE` / `$GOBIN`, npm `node_modules` + the pnpm content-addressed store,
  Bazel, Deno (`DENO_DIR`), Nix (`/nix/store`), and Zig (`zig-out/`).
- 2026-07-05 — review pass: an **exclusive lock** on the output directory for
  concurrent builds (Cargo locks `target/`); a **per-project segment**
  (`<out>/<project-id>/…`) when the output directory is shared, so co-located
  projects do not collide; **orphan synchronization** of `gen/` via an emitted-files
  manifest (a persisted cache must delete the `.go` of a removed `.aril`, or Go
  compiles a phantom); an `aril clean` command; a reserved cross-compilation slot
  (`bin/<target>/`); and corrected the `$ARIL_CACHE` description to
  *coordinate-addressed, content-verified* (aligning with RFC-0008).
- 2026-07-05 — `draft → accepted`, alongside RFC-0008 as its pair. The status
  flips to `implemented` when the `aril-out/` layout lands in `cmd/aril`.
- 2026-07-05 — `accepted → implemented`. Landed in `cmd/aril` over five PRs:
  the persisted `aril-out/{bin,gen}` layout + out-dir resolution precedence
  (the default binary moved from a loose `./<basename>` to `aril-out/bin/<name>`);
  emitted-files-manifest orphan synchronization of `gen/`; the exclusive
  out-dir flock + per-project namespacing under a shared out-dir; and the
  `aril clean` command (guarded against removing the project root). RFC-0008
  already owned `$ARIL_CACHE`; the reserved profile / cross-compilation slots
  remain deferred, and `aril test` is still its own future RFC.
