# Aril readiness audit — AUDIT-2 intuition sweep

The full model × rung × task × N sweep the AUDIT-1 pilot was built to size
(`harness.md`, `methodology.md`). AUDIT-1 validated the instrument on a thin
Haiku-only slice; AUDIT-2 runs the whole matrix — **4 models × 3 rungs × 10
tasks × N=3, one-shot *and* iterative** — against the fixed v0.21.0 surface, and
reports the intuition scoreboard, the diagnostic descent, the reference-language
control, idiom-divergence, and the findings that feed AUDIT-3/AUDIT-4.

Measurement-first: **no compiler change during the sweep**. The one compiler
*bug* the sweep surfaced (F-catch-discard, below) is logged here and fixed in a
separate follow-up, not mid-measurement.

## Setup

- **Instrument** — the AUDIT-1 harness (`harness.md`): fresh probe subagents,
  each given *only* its rung documents + a language-neutral task, graded by the
  deterministic compile/run oracle (`aril build --contracts=panic` + stdout
  compare — the same oracle `audit-runner` uses).
- **Axes** — models `haiku-4.5 · sonnet-5 · opus-4.8 · fable-5`; rungs
  **L1** (cheatsheet) · **L2** (+ `language-spec.md`) · **L3** (+ a curated
  worked-examples bundle); 10 tasks across 5 strata; **N=3** trials/cell; arms
  **one-shot** and **iterative** (compiler diagnostics fed back, cap K=4).
  360 cells/arm = **720 probes** total.
- **L3 worked-examples bundle** — four *different* compiling examples chosen to
  demonstrate idioms without being task solutions:
  `core-language/grade_classifier` (control-flow, `List`, closures, enumerate),
  `core-language/leaderboard` (`sort.sortedBy`, records, a contract),
  `modeling-errors/error_handling` (`Result`/`try`/`.mapErr`/`catch`),
  `stdlib-binding/statistics` (`sort.sorted(xs, less)`, `strconv`, `strings`).
- **Isolation** — one-shot probes read only staged rung docs + the task prompt
  (inline paths, no repo access); the iterative probes additionally run a
  sandboxed `check.sh` that compiles/runs their file and echoes the diagnostic
  (path-sanitized) or PASS — never the reference solution. One-shot isolation is
  tool-count-clean (Read-only of staged files); the iterative arm's is
  by-construction (no path to any reference; weaker than one-shot, noted).
- **Capture** — every probe *writes its own program to a cell-named file*
  (programmatic verbatim capture, the AUDIT-1 protocol lesson); grading reads
  those files. Raw submissions + records are archived out-of-tree
  (`scratchpad/a2/`), per `harness.md §8`; this scoreboard is the tracked finding.

## Reference-language control (task difficulty baseline)

Every task solved zero-shot in the languages the models *do* know:
**Go 10/10 · Rust 10/10** (TS control written but not machine-run — no toolchain).
Task difficulty is therefore nil; **every Aril miss below is Aril's surprise
surface, not task hardness** (`methodology.md §5`).

## Scoreboard — run_ok (compiles *and* correct output)

Headline (per rung, out of 120 = 4 models × 10 tasks × N=3):

| arm | L1 | L2 | L3 |
|---|---|---|---|
| **one-shot** | **100/120** (83%) | **107/120** (89%) | **118/120** (98%) |
| **iterative** (K≤4) | **114/120** | **113/120** | **120/120** |

Across the whole sweep, **`compile_ok == run_ok` in every single cell** (325/360
one-shot, 347/360 iterative): nothing that compiled produced wrong output. No
silent compiles-but-diverges appeared — a direct, strong signal for the governing
invariant *syntax must not lie*.

One-shot run_ok by **model × rung** (/30):

| model | L1 | L2 | L3 |
|---|---|---|---|
| haiku-4.5 | 23 | 26 | 28 |
| sonnet-5 | 24 | 27 | 30 |
| opus-4.8 | 26 | 27 | 30 |
| fable-5 | 27 | 27 | 30 |

The strength axis behaves as designed — weaker models trail at L1 (haiku 23 →
fable 27) and the spread closes to ~perfect by L3. The **weakest-passing-model ×
rung-of-first-success** classifier (`methodology.md §3`) puts *every* construct at
L1–L2 for all models **except** the two findings below.

One-shot run_ok by **stratum × rung** (/24 = 2 tasks × 4 models × 3):

| stratum | L1 | L2 | L3 |
|---|---|---|---|
| prior-aligned | 24 | 24 | 24 |
| trap | 23 | 24 | 24 |
| contract-authoring | 22 | 24 | 24 |
| aril-novel | 21 | 24 | 23 |
| library-lookup | 10 | 11 | 23 |

Only **library-lookup** is materially below ceiling, and it is one task
(`sort_desc`). Notably **trap = 23/24/24**: the CONTAINER-MODEL trap
(`accumulate_push` — "start empty, add one at a time") is **12/12 at every rung
for every model**, reaching for `List<int>{}` + in-place `.push` — the pre-v1
surface fix fully absorbed the trap.

## Diagnostic descent → findings

### F-sort — the dominant miss (docs/discoverability; api-lookup, no intuition penalty)

`sort_desc` one-shot: **L1 0/12, L2 0/12, L3 12/12** — *every* model fails at L1
and L2, *all* pass at L3. This is **24 of the 35 one-shot failures**. Every
failure is a clean Aril `E0217 "Module \`sort\` has no bound member …"` (19×
`slice`, 3× `Slice`, 1× `sort`, 1× `by`) — the Go/other-language idiom leaking;
Aril's spelling is `sort.sorted(xs, (a,b) => a > b)`. The rung docs L1/L2 don't
teach it (the cheatsheet lists `sort` only as an importable name; `sort.sorted`
lives in `docs/binding-surface.md`, which is not in L1/L2). By the taxonomy this
is an **api-lookup / doc-gap in the library-lookup stratum** — it does **not**
lower the intuition score (`methodology.md §5`). The L3 worked example
(`statistics.aril`, which shows `sort.sorted(xs, less)`) closes it completely.

**The decisive new datum — the iterative arm does *not* recover it.** With the
compiler in the loop (K≤4, some agents ran check.sh up to 8×), `sort_desc` L1/L2
is **all 13 of the iterative arm's failures**; every *other* task recovers. The
reason is direct: `E0217` correctly says `sort` has no member `slice`, but it
does **not suggest `sorted`**, so the agent cycles `slice→Slice→sort→by` and
gives up. **Empirical proof that the diagnostic is insufficient for recovery** →
the remediation is a *tailored `E0217` hint* ("`sort` has no `slice`; did you
mean `sort.sorted(xs, less)`?", the D38/D41 pattern) and/or a cheatsheet
trap-row. Confirms and sharpens AUDIT-1's residual across all four models.

### F-catch-discard — a real compiler bug (D10 violation) [FLAGSHIP]

`let n = expr catch _ { …diverge… }` (an **underscore-discard** `catch`)
miscompiles: codegen emits `_ := __aril_catch_1.E` — illegal Go
(*"no new variables on left side of :="*) — and the failure surfaces as a **raw
`go build` error** with a `//line` mapping past EOF, i.e. a **D10 violation** (an
error not in Aril terms). The named form `catch e { … }` emits `e := …` and works.
Minimal repro:

```aril
let n = strconv.atoi("5") catch _ { return }   // -> raw go build error
```

Surfaced by haiku on `parse_sum` at **L1, L2, and L3** (a compiler bug does not
recover with more docs — the diagnostic descent stays flat). It **does** recover
in the iterative arm — but only because the agent, unable to act on the raw Go
error, rewrites to `catch e` / `match`, *working around* the bug rather than
fixing its code. The fix is one line (emit `_ =` for a discard binding, or omit
the bind). **Fixed in a follow-up PR** (measurement-first keeps the sweep frozen).

### Minor L1 slips (clean diagnostics; recover at L2 and via iteration)

Each is a single clean Aril diagnostic, recovered by L2 docs *and* by one
iteration — the "cheatsheet under-teaches a corner" bucket, not a language flaw:

- **E0217 `strings.Fields`** (Go idiom; Aril `strings.fields`) — parse_sum, 1×. Same class as F-sort.
- **E0201 tuple-as-arg** — passed `(int,int)` where an `int` param was expected (called `divide(pair)` without destructuring) — checked_div, 2×.
- **E0112 `expected \`)\`, got \`:\`** — sum-type variant / labelled-arg syntax slip — shape_area L1, 2×.
- **E0103 `Unknown name contract`** — contract-block syntax under-taught at L1 — sonnet abs/clamp, 2×.
- **E0120** string-literal inside an interpolation hole — map_default, 1×.

## Idiom-divergence (blind classifier, model-anonymized, 325 compiling one-shot runs)

**304 intended / 21 awkward (93.5% idiom-conformant.)** The classifier (a strong
model, blind to which model wrote each program) rated every compiling submission.
The awkward cluster is narrow:

- **17× print-an-int via `strconv.itoa(i)`** instead of `fmt.println(i)` / `"${i}"`
  (fizzbuzz mostly). A **cheatsheet nit** — show that `fmt.println` takes an int
  directly and `${i}` interpolates ints.
- **3× bare `Map` index `counts[w]`** (returns the zero-value for a missing key)
  bypassing `.get(w).unwrapOr(0)` — compiles and gives the right answer but skips
  the `Option` the language intends. A **soft trap** (candidate hint).
- 1× an `if`-expression bound to a variable + itoa.

Everything else rated intended — including `Result` + `match` (checked_div 34/34),
the sum-type + exhaustive `match` (shape_area 34/34), and the `List<int>{}` +
`push` container idiom (accumulate_push, no slice workarounds).

## Learnability (iterative arm)

Recovery is cheap and near-total. Iterations-to-green (self-reported check.sh
runs, over 347 passing cells): **1→325, 2→15, 3→2, 4→16, 5→1, 8→1; mean 1.11.**
All 11 non-`sort_desc` one-shot failures reached green with feedback (the L1
syntax slips in 1 extra iteration; the `catch _` bug via a rewrite). The **only**
construct that resists the compiler loop is `sort_desc` at L1/L2 — a
diagnostic-quality gap, not a learnability gap.

## Verdict and hand-off

- **L1 headline 100/120 (83%) one-shot, 114/120 with one compiler round.** For
  the four intuition-relevant strata (prior-aligned, aril-novel, trap,
  contract-authoring — 8 tasks/96 cells) one-shot L1 is **90/96**, and the six
  misses are single clean-diagnostic slips that L2 or one iteration fixes.
- **No silent lies:** `compile == run` in all 720 cells.
- **Two things to fix**, both already scoped:
  1. **F-sort → a tailored `E0217` hint** pointing at `sort.sorted` (+ a
     cheatsheet trap-row). The iterative arm proves docs+diagnostic today can't
     guide recovery. → AUDIT-4 remediation (compiler `hint` tier) + a cheap doc row.
  2. **F-catch-discard → a one-line codegen fix** (`_ :=` → `_ =`). A D10 hard-
     constraint violation on idiomatic code. → immediate follow-up PR.
- **Doc nits for the gotchas/cheatsheet backlog:** print-an-int (`fmt.println(i)`
  / `${i}`, not `itoa`); bare `Map` index bypasses `Option`.
- **AUDIT-3 (trap hunt)** inherits: the prior-leakage cluster is almost entirely
  `sort.*` (Go idioms) + `strings.Fields`/`Fields`-casing + the bare-Map-index
  soft trap; adversarial top-up should probe other `stdlib` spellings and
  zero-value-vs-Option surfaces. **F-else** (AUDIT-1 provisional) did **not**
  recur in any of the 720 cells (no `}`-newline-`else`), including Fable — a clean
  signal, though not an exhaustive refutation.
