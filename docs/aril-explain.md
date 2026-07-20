# `aril explain` — native Aril panic traces (design note)

**Status: exploratory — v0 implemented (`aril explain`), v1 not yet.** This is the
*playground* for the mechanism — built and played with here before it is
formalised. The **v0** post-hoc text filter (`cmd/aril/explain.go`) ships and is
demonstrated below; the **v1** symbol sidecar, `aril guard`, and the in-process
`recover` are not yet built. **RFC-0011 is reserved** for the eventual
formalisation (number claimed in `docs/rfcs/`, RFC deliberately not written yet);
this note graduates into it once the design is validated in practice.

Demonstrated on the real traces the AUDIT-3 hunt surfaced (`./prog 2>&1 | aril explain`):

```
# integer 10/0
panic: runtime error: integer divide by zero        panic: division by zero
goroutine 1 [running]:                          ⇒     at main  (p1.aril:8)
main.main() … /…/p1.aril:8 +0x9

# user method + arilrt frame (index-out-of-range)
panic: runtime error: index out of range [50] …     panic: index out of range [50], length 2
aril-output/arilrt.(*List[...]).At(…) …         ⇒     at Grid.at  (um.aril:8)
main.(*Grid).at(…) … /…/um.aril:8                     at main     (um.aril:13)
main.main() … /…/um.aril:13                           … 1 internal frame(s) hidden
```

Note even v0 renders the method frame natively (`main.(*Grid).at` → `Grid.at`, a
syntactic heuristic) and hides the arilrt frame by its `gen/arilrt/*.go` path — no
symbol map needed; v1 covers only the harder mangled/closure frames.

## Why

Aril compiles to Go, so a *runtime* panic — integer divide-by-zero, index / slice
out of range, nil dereference, send-on-closed-channel — surfaces with Go's own
message and goroutine trace:

```
panic: runtime error: index out of range [50] with length 2

goroutine 1 [running]:
aril-output/arilrt.(*List[...]).At(...)
	…/aril-out/gen/arilrt/containers.go:183
main.(*Grid).at(...)
	…/um.aril:8
main.main()
	…/um.aril:13 +0xcc
exit status 2
```

The compile-time diagnostic boundary is Aril-clean (every sema/type error is an
`error[Exxxx]` in `.aril` coordinates and terms). The *runtime* boundary is not:
only the **coordinate** of each user frame is already Aril (the `//line`
directives map it — `um.aril:8`, `um.aril:13` above); the **message text**, the
**frame symbols** (`main.(*Grid).at`), the **Go-internal frames**
(`arilrt.(*List[...]).At`, `runtime.*`), and the `goroutine … [running]` / PC
offsets / `exit status` are all raw Go.

`aril explain` reframes that block into a native Aril view **after the fact**. It
is deliberately a *presentation* tool: it does not change what a panic *is* (that
is the open panic-semantics question — Result-ify? in-process abort handler?),
so it is available now without settling that design. Presentation and semantics
are separable, and the developer's actual complaint here is presentation.

## The command

```
aril explain [--binary <path>] [<file>]
./prog 2>&1 | aril explain
aril explain < panic.txt
```

- **Post-hoc, runs nothing.** It reads a captured trace from stdin (or a file
  argument), translates it, and prints the result. This is the general shape: it
  works on *any* captured trace — a fresh crash, a deployed-binary log, a CI
  failure, a stack a colleague pasted — with no process wrapping.
- **Exit code 0** on success (it is analysing text, not running a program). It is
  a filter: if the input is not a recognisable Go panic block, it echoes the
  input unchanged (never worse than the raw trace).
- `--binary <path>` points at the built binary / its `aril-out/` so the symbol
  map (tier v1 below) can be loaded; otherwise the map is auto-discovered from the
  `.aril` / `aril-out` paths present in the trace, and absent that, v0 runs.

A thin `aril guard <prog> [args…]` sugar (run the program, and on a panic-crash
auto-run `aril explain` over its stderr) is a **future** convenience on top of the
same engine — named `guard`, not `run`, to mark it a debug mode, not the standard
launch. An `arilrt`-level in-process `recover()` that reframes without any external
tool (covering a directly-run `aril build` binary) is a **later, deferred** step —
the only part of bug#5 that waits on the panic-semantics design.

## What it produces

The same trace, reframed:

```
panic: index out of range [50], length 2
  at Grid.at   (um.aril:8)
  at main      (um.aril:13)
```

- the **message** is translated to Aril phrasing (fixed table below);
- only **user frames** are shown, each with its already-Aril `file:line`;
- **internal frames** (arilrt, Go runtime, synthesised) are collapsed;
- the **frame symbol** is rendered as an Aril name (`main.(*Grid).at` → `Grid.at`),
  which is the one part that needs the per-binary symbol map (tier v1).

## Two tiers

The engine is the same; the difference is whether a symbol map is available.

### v0 — no map, pure text filter (works on any pasted trace)

Needs zero per-binary context, so it is the first thing to ship and the flow the
"I saw a confusing error, let me run it through the translator" use-case wants:

1. **Translate the message** via the fixed table.
2. **Classify frames by their source path** — the path *itself* is the
   user/internal discriminator, no map required:
   - a `.aril` file → a **user** frame (coordinate already native) → keep;
   - `…/gen/arilrt/*.go`, `…/gen/main.go` internals, `runtime.*`, and the
     `goroutine … [running]` header / PC offsets / `exit status` tail →
     **internal** → drop (or collapse to a single `… N internal frames`).
3. **Clean the kept symbols heuristically** — strip the `main.` package prefix
   (`main.main` → `main`, `main.deeper` → `deeper`). A **free function** lowers to
   `main.<arilName>` (near-identity), so v0 already renders it natively; a method
   frame stays `main.(*Grid).at` (Go-ish but readable) until v1.

### v1 — with the symbol map (upgrades methods / generics / closures)

A per-binary **symbol sidecar** maps each generated Go symbol back to its Aril
qualified name — the frames v0 cannot reverse from the spelling alone:

| Go symbol | Aril name |
|---|---|
| `main.(*Grid).at` | `Grid.at` |
| `main.main.func1` (closure / IIFE / spawn body) | `<closure @ file:line>` |
| a `goIdent`-mangled name (reserved-word collision, the `_arilSelf` family) | its source name |
| `arilrt.(*List[...]).Push` (library) | collapsed as internal |

- **Source:** codegen already computes the Aril-name → Go-symbol mapping when it
  emits the program, so the reverse table is free to produce.
- **Location:** a sidecar under `aril-out/` next to the codegen output (e.g.
  `aril-out/gen/<name>.arilmap`), emitted at `aril build` / `aril run`; the
  persisted-`gen/` layout (RFC-0009) already keeps codegen artifacts there.
  `aril explain` finds it from the trace's `aril-out` path or `--binary`.
- **Format:** a small, stable, line-oriented table (Go symbol ⇒ Aril name +
  kind), version-tagged so a stale map degrades to v0 rather than mis-translating.

## The message table (fixed, in the tool)

The Go runtime error strings are a small closed set, so the table lives in the
tool — no per-binary data:

| Go runtime message | Aril rendering |
|---|---|
| `runtime error: integer divide by zero` | `division by zero` |
| `runtime error: index out of range [i] with length n` | `index out of range [i], length n` |
| `runtime error: slice bounds out of range …` | `slice bounds out of range …` |
| `runtime error: invalid memory address or nil pointer dereference` | `nil dereference` |
| `close of closed channel` | `channel already closed` |
| `send on closed channel` | `send on a closed channel` |
| `runtime error: negative shift amount` | `negative shift amount` |

(Exact wording is finalised when implemented; an unrecognised message passes
through verbatim.)

## Scope & non-goals

- **No semantics change.** `aril explain` never alters program behaviour, exit
  codes, or what operations do at runtime; it only re-renders a captured trace.
  The panic-semantics question (Open-Problem-#3) is orthogonal and untouched.
- **Best-effort.** User frames render natively; internals are collapsed (as any
  good runtime hides its own frames). A frame the map cannot resolve degrades to
  its cleaned Go spelling, never an error.
- **`aril guard` and in-process `recover`** are future surfaces over the same
  engine (see *The command*); this note specifies the engine and `aril explain`.

## Relation to bug#5

This is the intermediate remediation for the AUDIT-3 compiler-bug *"runtime
panics carry raw Go text"* (`docs/audit/audit3.md`, T11 for the `10/0`
honest-difference). It closes the *presentation* half now; the *semantics* half
stays with the panic-semantics redesign. The user-facing "runtime panics show
Go-level text, run them through `aril explain`" caveat belongs on the gotchas
page (R4 TRAP-DOCS), pointing here.
