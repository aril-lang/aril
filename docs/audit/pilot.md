# Aril readiness audit — AUDIT-1 pilot results

The AUDIT-1 pilot: a thin slice run through the harness (`harness.md`) to validate
the instrument end-to-end, exercise the protocol, and surface the first findings
before the full AUDIT-2 sweep. This is the pilot's scoreboard + finding list +
the calibration/protocol lessons that shape AUDIT-2.

> **This document has two passes.** The **original pilot** (below, against the
> v0.19.0 surface) is preserved as-is for the before/after record. The
> **re-run** (against the fixed v0.21.0 surface, after the LANGUAGE-FIXES and
> CONTAINER-MODEL epochs) is the current headline — jump to
> [§ AUDIT-1 re-run](#audit-1-re-run-fixed-surface-2026-07-13).
>
> **AUDIT-2 (the full model × rung × task × N sweep this pilot sized) has run** —
> see [`audit2.md`](audit2.md) for the intuition scoreboard, the iterative arm,
> and the findings.

**Scope.** Measurement-first: no compiler change. The one remediation applied is a
doc calibration (the cheatsheet fix below), validated before/after; all other
findings are logged for AUDIT-4.

## Setup

- **Instrument** — `audit/runner` (the deterministic compile/run oracle) + probe
  subagents (`harness.md §3`). Each probe is a fresh agent given *only* the rung
  document(s) and a language-neutral task; it returns `.aril` source, which the
  oracle grades against the task's `expected.txt`.
- **Isolation** — the probe reads *only* the staged rung file(s); verified per
  probe by the tool-use count (1 read at L1, 2 at L2, no repository access).
- **Cells run** — the Haiku 4.5 endpoint at L1 (cheatsheet only) and L2
  (cheatsheet + `docs/language-spec.md`), 10 tasks each, one-shot. **This is
  thinner than the `harness.md §7` pilot slice** (two model endpoints, N≈3): the
  slice was further reduced to Haiku-only / N=1 to de-risk the instrument first;
  the second endpoint (Fable 5) and N>1 trials move to AUDIT-2.

## Scoreboard (Haiku 4.5, one-shot)

| rung | compiling / 10 | dominant failure cause |
|---|---|---|
| **L1** (cheatsheet, as-shipped) | **0** | doc omission — no `import` lines, no `func main()` skeleton |
| **L2** (+ `docs/language-spec.md`) | **4** | genuine language surprises (the spec supplies imports/main) |
| **L1** (cheatsheet, *after* the doc fix) | **7** | genuine language/library surprises only |

**Provenance.** All three numbers come from the *clean* runs: the real rung doc(s)
read **verbatim**, byte-identical per probe, isolation verified by tool-use count.
An earlier exploratory pass that hand-condensed the rung doc per task/model is
**excluded** (it confounded the context — see protocol lesson 1). The one
transcription slip (lesson 2) was caught and corrected, so the fixed-L1 figure is
7, not 6.

**Comparability caveat.** The three cells are **not** a single controlled series:
`L1-as-shipped` and `L2` share the pre-fix cheatsheet, while `L1-after-fix` uses the
*fixed* cheatsheet — the rows straddle the doc-fix boundary. So the `7 > 4`
(fixed-L1 over L2) is **not** "one page beats the full spec"; the two cells aren't
apples-to-apples. The sound reads are the two within-boundary comparisons:
`L1-as-shipped 0 → L2 4` (spec recovers the doc gaps) and
`L1-as-shipped 0 → L1-fixed 7` (the doc fix recovers them). AUDIT-2 re-runs L2
against the fixed cheatsheet to close the gap.

The **diagnostic descent** (`methodology.md §3`) reads cleanly: an L1 failure that
an L2 (or a doc fix) recovers is a **docs** problem, not a language-design one. The
0→7 L1 lift from a single doc section localizes the headline gap to the cheatsheet.

(The cheatsheet fix *is* remediation, normally batched to AUDIT-4 by measurement-first
— applied here as the deliberate exception the before/after descent required, and
scoped to docs so the compiler stays frozen.)

## Findings (frequency-ranked; remediation bucket in brackets)

1. **F-import** *[docs — FIXED + validated]* — the cheatsheet used `fmt.println`
   throughout but showed zero `import` lines, so every probe omitted imports →
   `undefined: fmt`. Fixed by the "A complete program" section; Haiku L1 0→7.
2. **F-main** *[docs FIXED + validated; compiler follow-up]* — probes wrote
   `func main(): unit`; the compiler rejects it. Doc side fixed (skeleton shown).
   **Compiler side:** the rejection surfaces as a *raw Go diagnostic*
   ("func main must have no arguments and no return values"), not an Aril-source
   diagnostic — see F-godiag.
3. **F-godiag** *[compiler — candidate D10 gap, follow-up]* — several failures surface with
   **Go terminology** (`undefined: fmt`, the main-signature message) rather than an
   Aril diagnostic in Aril terms. They carry `.aril` coordinates (via `//line`) but
   the *message* is Go's. Candidate D10 ("errors reported in Aril terminology") gap;
   a natural home for a future `hint`/diagnostics improvement.
4. **F-else** *[trap/docs — provisional]* — `else` placed on a new line after `}`
   → E0112; Aril follows Go's `} else {` same-line rule. Evidence: a **Fable 5**
   probe genuinely emitted `}`-then-newline-`else`. (Note: my transcription slip in
   lesson 2 *coincidentally* produced the same shape in a Haiku submission — that
   instance is operator error, corrected, and is **not** counted here.) Provisional
   until AUDIT-2 re-confirms it in a clean verbatim-capture run.
5. **F-stdlib-leak** *[trap/docs]* — Go/TS stdlib idioms leak: `fmt.Println`/
   `fmt.Printf` (capitalized), `sort.slice`/`sort.Slice`/`slices.SortFunc`,
   `text.split` (method form). Aril spellings: `fmt.println`, `sort.sorted(xs, less)`,
   `strings.split`.
6. **F1 (Result arity)** *[docs]* — `Result<int>` (single arg) → E0207; the type
   needs `Result<int, error>`. (The cheatsheet's "`Result<T>` defaults E = error"
   note over-promises relative to the compiler — a doc/compiler mismatch to resolve.)
7. **F2 (empty collection literal)** *[docs]* — `var xs: []int = []` → E0112; the
   empty literal is `[]int{}`. The cheatsheet shows non-empty literals only.

## Protocol lessons for AUDIT-2 (the pilot's other job)

- **Uniform rung context is load-bearing.** A first pass hand-condensed the rung
  doc differently per task/model, confounding the model comparison. Fix adopted:
  the rung is the *real* doc read verbatim (`docs/cheatsheet.md`, `docs/language-spec.md`),
  byte-identical for every probe. AUDIT-2 must hold the rung constant across cells.
- **Capture the submission verbatim — never retype it.** Manually transcribing a
  probe's fenced block into a file introduced a transcription error (a correct
  `} else if` became else-on-newline → a false compile-fail). AUDIT-2's harness
  must extract the ` ```aril ` block programmatically. The oracle is sound; the
  *capture* step was the leak.
- **Isolation via tool-use count works** as a cheap, verifiable guard (probe reads
  only the rung file; count == expected).

## Human-calibration package (methodology §10.B)

The confirmed LLM misses above are the seed of the human-newcomer calibration set:
each is a task + rung doc (no answer) a human volunteer can attempt, to check that
an LLM miss proxies a human miss before AUDIT-2 trusts the intuition sub-score. The
package is the task bank (`audit/tasks/`) paired with the two rung docs; the misses
to calibrate first are the highest-frequency **confirmed** ones — F-import and
F-main (F-else is provisional pending a clean AUDIT-2 re-confirmation, so it joins
the calibration set only once confirmed).

## What AUDIT-2 does with this

- Hold the rung constant; capture submissions verbatim; add Fable 5 + N trials.
- Re-run the L1 cell against the *fixed* cheatsheet as the new baseline (the 7/10
  above is the provisional new floor).
- Feed F-godiag to a compiler diagnostics ticket; feed F-else/F-stdlib-leak/F1/F2
  to the trap catalog and doc backlog.

---

## AUDIT-1 re-run (fixed surface, 2026-07-13)

The pivot the original pilot recommended — *fix the language/docs first, then
re-measure* — has landed: the **LANGUAGE-FIXES** epoch (v0.20.0: `Result<T>`
defaults `E = error`; `Option.map`/`Result.map`; `Map .has/.get` documented;
`else`/`catch` diagnostic) and **CONTAINER-MODEL** (v0.21.0: honest `List<T>`,
`[]T` loses `.push`, cheatsheet + corpus migrated). This re-run measures the
**fixed** surface with the same instrument, holding the protocol lessons below.

**Scope.** Same thin slice as the original pilot: **Haiku 4.5**, one-shot, N=1,
10 tasks × {L1, L2}. The rung docs are the *current* `docs/cheatsheet.md` (L1)
and `+ docs/language-spec.md` (L2), read **verbatim**, byte-identical per probe.
Submissions captured **programmatically** from each probe's fenced block (never
retyped — protocol lesson 2). Isolation verified by tool-use count: **all 10 L1
probes made exactly 1 read (cheatsheet); all 10 L2 probes exactly 2 reads (both
docs); zero repository access.**

### Instrument note — the runner needed a CONTAINER-MODEL migration first

`audit/runner` and one fixture used slice `.push`, which CONTAINER-MODEL removed
(E0214), so `audit-runner validate` was red until migrated. Fixed as part of this
re-run: `audit/runner/main.aril` two accumulators → `List<T>` + `.toSlice()` at
the `[]T` boundary; `audit/tasks/trap/accumulate_push/reference.aril` → the
corrected `List<int>{}` + in-place `.push` idiom (this trap task's *intended*
answer is now exactly the CONTAINER-MODEL idiom). Bank re-validated **10/10 green**
before any probe ran.

### Scoreboard (Haiku 4.5, one-shot, fixed surface)

| rung | compiling+running / 10 | vs. original pilot |
|---|---|---|
| **L1** (fixed cheatsheet) | **9** | 7 → **9** |
| **L2** (+ `docs/language-spec.md`) | **9** | 4 → **9** |

> **On the deltas.** The "vs. original pilot" column is *not* a controlled series —
> it folds two changes: the AUDIT-0 cheatsheet fix *and* the v0.20.0/v0.21.0
> surface fixes. The original `L2 = 4` was measured against the *pre-fix* cheatsheet
> (+ spec); this `L2 = 9` uses the *fixed* cheatsheet (+ spec). The sound reading is
> the absolute re-baseline (both rungs now 9/10 on the fixed surface), not the raw
> arrow. This is the deliberate "re-run against the fixed surface" AUDIT-2 was told
> to establish, superseding the original pilot's straddled-boundary caveat.

Per stratum (run_ok / 2), identical at both rungs:

| stratum | L1 | L2 |
|---|---|---|
| prior-aligned | 2/2 | 2/2 |
| aril-novel | 2/2 | 2/2 |
| trap | 2/2 | 2/2 |
| contract-authoring | 2/2 | 2/2 |
| library-lookup | 1/2 | 1/2 |

The **single** failure at each rung is the same task — `library-lookup/sort_desc`
— for the same reason (below). Every **intuition-relevant** stratum
(prior-aligned, aril-novel, trap, contract-authoring — 8 tasks) is **8/8 at both
rungs**: the surface fixes closed the headline gaps a weak model hit.

### Findings delta

- **F-import / F-main — CONFIRMED FIXED.** Zero `undefined: fmt` and zero
  `func main` rejections across 20 probes; every submission imported correctly and
  used the `func main() {…}` skeleton. The AUDIT-0 cheatsheet fix holds under
  re-measurement.
- **F1 (Result arity) — FIXED, end-to-end.** The L2 `checked_div` probe wrote
  `func divide(...): Result<int>` (single type arg) with `Err(error("…"))` and it
  **compiled and ran** — the `Result<T>` → `Result<T, error>` normalization
  (v0.20.0) works from a probe, not just in docs.
- **CONTAINER-MODEL trap navigated — new positive.** `trap/accumulate_push` ("start
  from an empty list and add one at a time") — the exact `.push`-lies trap that
  motivated Epoch B — was solved **correctly at L1** (cheatsheet only) by Haiku,
  reaching for `List<int>{}` + in-place `.push`. Teaching `List<T>` in the
  cheatsheet + the loud E0214 turned a trap into a non-event for a weak model.
- **Map default lookup — surfaced.** `trap/map_default` used `Map<…>{}` +
  `.get(k).unwrapOr(0)` — the `.get` the v0.20.0 doc fix (#180) exposed.
- **F-stdlib-leak — PERSISTS (the sole residual failure).** Both `sort_desc`
  probes reached for Go's `sort.Slice(xs, less)`; Aril's spelling is
  `sort.sorted(xs, less)`. **Neither rung doc teaches it** — the cheatsheet lists
  `sort` only as an importable module name, and `sort.sorted` lives in the module
  API reference (`docs/binding-surface.md`), which is *not* in L1/L2. So this is an
  **api-lookup / doc-gap** in the `library-lookup` stratum (which grades the
  self-service protocol, not intuition; api-lookup misses do **not** lower the
  intuition score, methodology §5). The diagnostic is **clean and Aril-coordinate**
  — `E0217: Module `sort` has no bound member `Slice`` — *not* a raw go/types leak
  (so F-godiag did **not** recur here). It just doesn't yet *suggest* the right
  member.
- **F-godiag — not observed in this re-run.** The only failure produced a clean
  E0217, not Go terminology. (F-godiag's original triggers were F-import/F-main,
  both now fixed.) It remains a live compiler-diagnostics candidate for the
  raw-`go build` paths, to be re-probed in AUDIT-2's broader/L3 sweep.
- **F-else — still provisional.** Not retriggered (it was a Fable-5 observation;
  this Haiku-only re-run neither confirms nor refutes it). Re-confirm in AUDIT-2
  with Fable + verbatim capture.

### Remediation candidates (measurement-first — logged, not applied)

1. **`sort.sorted` discoverability (F-stdlib-leak).** Cheapest: a cheatsheet
   trap-table row (`sort.Slice`/`slices.SortFunc` → `sort.sorted(xs, less)`),
   consistent with the existing `fmt.Println`→`fmt.println` guidance. Stronger: a
   tailored **E0217 hint** on a `sort.Slice`/`sort.slice` miss pointing at
   `sort.sorted` (the D38/D41 tailored-diagnostic pattern) — a compiler change, so
   batched to AUDIT-4.
2. **Library-lookup rungs need the API reference.** L1/L2 exclude
   `docs/binding-surface.md`, so a `library-lookup` task whose call name diverges
   from the Go one is unwinnable at those rungs by construction. AUDIT-2's **L3**
   (adds a curated `examples/` slice) and the self-service protocol are where these
   are meant to clear; the pilot confirms the stratum behaves as designed
   (`parse_sum` won only because `strconv.atoi` matches the Go name).

### Protocol confirmation (the pilot's other job)

All three AUDIT-1 protocol lessons held cleanly this run: the rung was the *real*
doc read **verbatim** (byte-identical per probe); every submission was captured
**programmatically** from its fenced block (no transcription step, so no repeat of
the original pilot's transcription slip); **isolation was verified by tool-use
count** (1 read @L1, 2 @L2, uniformly). Raw records:
`records_L1.jsonl` + `records_L2.jsonl` (harness §1 schema, `model=haiku-4.5`),
archived out-of-tree with the per-probe submissions.

### What this hands AUDIT-2

- **New floors:** L1 and L2 are both **9/10** on the fixed surface (was 7 and 4).
  The one gap is a known api-lookup miss, not an intuition failure.
- **Add the remaining axes** (methodology): Fable 5 (+ re-confirm F-else), N>1
  trials, the **L3** rung (which should clear `sort_desc` via the API reference /
  worked examples), and the iterative arm.
- **Trap-catalog / doc backlog:** F-stdlib-leak (`sort.sorted`) is the one carried
  finding; F-godiag stays a compiler-diagnostics candidate to re-probe on the raw
  `go build` paths at L3.
