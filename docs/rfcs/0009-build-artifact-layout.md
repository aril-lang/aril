# RFC-0009 — Build-artifact layout

| Field | Value |
|---|---|
| Number | 0009 |
| Status | draft |
| Created | 2026-07-05 |
| Supersedes | — |

## Summary

Define **where Aril writes build artifacts** and **where it caches
dependencies**. A per-project **`aril-out/`** directory (visible, git-ignored)
holds the final binary (`aril-out/bin/<name>`) and the persisted lowered Go
(`aril-out/gen/`). The dependency cache is the **global content-addressed
`$ARIL_CACHE`** (RFC-0008) — shared across projects, never copied per-project.
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
  `aril build`. An explicit `-o <path>` overrides it to an arbitrary path.
- **`aril-out/gen/`** — the lowered Go module. Aril lowers here and **keeps it**
  between builds rather than lowering to a throwaway temporary directory.
- **`aril-out/.gitignore`** — auto-generated, containing `*`, so every artifact
  is ignored even if the developer forgets to add `/aril-out` to the project's
  own `.gitignore` (the discipline Cargo applies to `target/`).

`aril-out/` is visible (not a hidden `.aril/`): the primary goal is
discoverability for a TypeScript-refugee audience that already reads a top-level
`dist/`, and a visible directory answers "where is my binary?" at a glance.

### Persisted lowered Go — the incremental-build payoff

The lowered Go in `aril-out/gen/` is retained across builds. Go's build cache
(`$GOCACHE`) is content-addressed, so an unchanged `gen/` yields a cache hit and
the rebuild is incremental instead of a full recompile of a fresh temporary
module. Go remains an intermediate representation (D1) — persisting it makes it
*present* (a debugging window into the IR), not readable-as-a-goal.

### The dependency cache — global and content-addressed

Fetched dependency modules live in the **global** content-addressed
`$ARIL_CACHE` (RFC-0008), shared across every project on the machine and **never
copied into `aril-out/`**. This is the pnpm-store / Go-module-cache / Nix-store
model — the relief from the per-project duplication of `node_modules`. If
per-project visibility of dependencies is ever wanted, the answer is to *link*
(pnpm-style), never to copy.

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

## Alternatives considered

Grounded in a prior-art pass over Cargo, Go, npm/pnpm/Yarn, Bazel, Deno, Nix, and
Zig.

- **A per-project build dir (Cargo `target/`) vs. no project dir (Go).** Go's
  no-directory model drops the binary in the working directory (or `$GOBIN`),
  which is the source-tree-litter problem; it has no obvious `.gitignore` target.
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
- **A global content-addressed cache (pnpm store, Go module cache, Nix) vs.
  per-project vendored copies (npm `node_modules`).** Per-project copies are the
  `node_modules` duplication the TypeScript audience is fleeing. A single global
  content-addressed store (`$ARIL_CACHE`) is chosen; dependencies are never copied
  per-project.
- **Persisting the lowered Go vs. a throwaway temp directory.** Lowering to
  `os.MkdirTemp` and deleting it keeps generated Go fully out of sight (strict
  IR-opacity) but forces a full recompile every build and offers no IR-debugging
  window. Persisting to `aril-out/gen/` unlocks Go's incremental `$GOCACHE` and a
  debugging window, at the cost of a cleanup surface. Persisting is chosen; Go
  stays an IR (D1) — present, not advertised as readable.
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
`ARIL_OUT` to a scratch directory so it does not create an `aril-out/` beside
each example (which the corpus enumeration would otherwise see). This is the one
integration point the persisted default introduces.

This RFC is the build-artifact peer of RFC-0008 (the dependency & build system);
RFC-0008 owns `$ARIL_CACHE` and the dependency model, this one owns the
per-project output layout. A user-facing test surface (`aril test`) is a separate
concern with its own RFC.

## History

- 2026-07-05 — drafted, as the build-artifact-layout peer of RFC-0008, grounded in
  a prior-art pass over Cargo `target/` + `CARGO_HOME`, Go's `$GOCACHE` /
  `$GOMODCACHE` / `$GOBIN`, npm `node_modules` + the pnpm content-addressed store,
  Bazel, Deno (`DENO_DIR`), Nix (`/nix/store`), and Zig (`zig-out/`).
