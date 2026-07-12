# Aril readiness audit — AUDIT-1 pilot results

The AUDIT-1 pilot: a thin slice run through the harness (`harness.md`) to validate
the instrument end-to-end, exercise the protocol, and surface the first findings
before the full AUDIT-2 sweep. This is the pilot's scoreboard + finding list +
the calibration/protocol lessons that shape AUDIT-2.

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
