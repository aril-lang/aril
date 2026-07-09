# Aril readiness audit — the AUDIT-1 harness

Operationalizes the methodology (`methodology.md`) into a concrete, runnable
instrument. The methodology says *what* to measure and *why*; this document
pins down the *how*: the record schema, how each context rung is materialized,
the probe-subagent protocol, the correctness oracle, the blind error-taxonomy
classifier, and the pilot that sizes the full sweep.

**Scope of AUDIT-1.** Build the instrument and *pilot* it — a thin slice across
the axes to validate the loop end-to-end, calibrate the trial count `N`, and
prune obviously-flat cells before the full spend. The full model × rung × task ×
N sweep is AUDIT-2. Nothing here changes the compiler (measurement-first: the
target is fixed).

## 1. One run = one cell sample

A **run** is a single (task, model, rung, feedback-arm, trial) tuple executed
once. It emits exactly one record. The harness is a function over the axes of
`methodology.md §2`:

```
run(task, model, rung, arm, trial) -> record
```

Runs are independent and order-free — the design is embarrassingly parallel,
but AUDIT-1 executes a *small* pilot slice serially-or-lightly-parallel to keep
resource use bounded (see §7). Fan-out to the full matrix is deferred to
AUDIT-2.

### The record schema

One JSON object per run (the pilot writes JSONL; one line per record):

| field | type | source |
|---|---|---|
| `task_id` | string | task bank; anchored to a `lang-spec/` artifact ID |
| `stratum` | enum | `prior-aligned` \| `aril-novel` \| `trap` \| `library-lookup` \| `contract-authoring` |
| `model` | enum | `haiku-4.5` \| `sonnet-5` \| `opus-4.8` \| `fable-5` |
| `rung` | enum | `L1` \| `L2` \| `L3` \| `L4` (methodology §2 axes) |
| `arm` | enum | `one-shot` \| `iterative` |
| `trial` | int | 0-based repeat index within the cell |
| `language` | enum | `aril` \| `go` \| `ts` \| `rust` (reference control, §5) |
| `compile_ok` | bool | deterministic oracle (§4) |
| `run_ok` | bool\|null | deterministic oracle; null if it never compiled |
| `iterations` | int | attempts to green in the iterative arm (1 in one-shot) |
| `error_class` | enum\|null | blind classifier (§6); null on first-try success |
| `idiom_divergence` | enum | `intended` \| `awkward` \| `n/a`; blind classifier |
| `submission` | string (path) | the agent's final source, archived for audit |
| `transcript` | string (path) | the probe subagent's trajectory, archived |

`error_class` values mirror `methodology.md §5`: `syntax-semantics`,
`api-lookup`, `missing-feature`, `prior-leakage`, `doc-gap`, `doc-misled`. The
routing in §5 (API-lookup misses never lower the intuition score) is applied at
*aggregation*, not capture — the record stores the raw class; the scoreboard
applies the routing. This keeps the raw data re-analysable if the routing rules
change.

## 2. Materializing the context rungs

A rung *is* a set of documents handed to the probe subagent as its entire
knowledge of Aril — nothing else about the language is in its prompt. The docs
are real, tracked artifacts, so **the docs are on trial alongside the language**
(methodology §5, delta C):

| rung | context handed to the probe |
|---|---|
| **L1** | `docs/cheatsheet.md` only (the one-page teaching floor) |
| **L2** | L1 + `docs/language-spec.md` |
| **L3** | L2 + a curated slice of `examples/` (worked idioms) |
| **L4** | L3 + the compiler itself (iterative arm: errors fed back) |

The rung ladder is **cumulative and monotone** — each rung strictly adds
context — so "rung of first success" is a well-ordered classifier (§3 table in
methodology). L4 is only meaningful in the iterative arm; the one-shot arm tops
out at L3.

**Isolation is the whole measurement.** The instrument's validity rests on the
probe carrying Go/TS/Rust priors but **zero Aril priors** (methodology §2). Each
run therefore spawns a *fresh* subagent with no memory of prior runs and a
prompt containing only (a) the rung's documents, (b) the task, (c) the output
contract. The harness must never leak: another task's solution, a previous
trial, the intended idiom, or the phrase "Aril" attached to any training-corpus
association beyond what the rung docs state. A reused or context-polluted agent
silently converts a prior-transfer measurement into a learning measurement.

## 3. The probe-subagent protocol

Each run drives one subagent (the `Agent` tool; the four model tiers are the
strength axis). The prompt template:

1. **Framing** — "You are writing a program in Aril, a language you will learn
   *only* from the documents below. Use your general programming knowledge for
   algorithms, but do not assume Aril syntax/semantics beyond these documents."
2. **Context** — the rung's documents verbatim (§2).
3. **Task** — the task statement + the I/O contract the oracle will check.
4. **Output contract** — return *only* the `.aril` source (a fenced block), no
   prose. In the iterative arm, the harness compiles/runs, feeds back the raw
   Aril diagnostic (source-coordinate, per D10), and re-prompts, up to a capped
   iteration count.

The probe is a *user* of Aril, not a contributor: it gets no repo access, no
`AI.md`/`CLAUDE.md`, no pipeline files — only the rung docs. This is enforced by
constructing the prompt from file *contents* (not paths into the repo) so the
agent cannot wander the tree and read the answer.

**Agent-audience note (methodology §2).** Because code-agents are a first-class
target population, a low probe score is a first-class defect, not merely a proxy
signal. The `skills/aril-authoring` skill is *not* given at the intuition rungs
(it would contaminate the prior-transfer measurement); it is measured separately
as its own rung variant in AUDIT-2's agent-authoring track.

## 4. The correctness oracle (deterministic, blind)

Correctness is graded by the existing corpus machinery (methodology §8, reuse of
D25/D34), never by an LLM:

- **compile** — the harness writes the submission to a temp module and runs
  `aril build`. Exit 0 → `compile_ok`.
- **run** — execute the built binary; compare stdout against the task's
  expected-output sidecar (exact, or ordered-subsequence for concurrent tasks,
  exactly as the corpus run-oracle does today). Match → `run_ok`.

Each task ships an expected-output sidecar in the same format the corpus uses,
so the oracle is a thin reuse of `tools/corpus-status`' run logic, not a new
grader. Determinism here is what lets the error-taxonomy classifier (§6) stay
the *only* model-judgment in the loop.

## 5. Reference-language control

Every task also carries a reference statement runnable in Go / TS / Rust — the
languages the models *do* know. The same (model, rung-equivalent, arm) runs the
reference task; a model that writes correct Go zero-shot but fails Aril isolates
Aril's surprise surface from raw task difficulty (methodology §5). The reference
oracle is the language's own toolchain (`go run`, `tsc`+`node`, `cargo`),
wrapped behind the same record schema (`language` field). Reference runs are
one-shot only — they calibrate task difficulty, not Aril learnability.

## 6. The error-taxonomy classifier (blind, separate model)

The one model judgment in the loop. A strong model classifies each *failing*
first attempt into `error_class` + `idiom_divergence`, and it runs **blind**: it
sees the task, the rung docs, and the submission, but **not** which model
produced the code (methodology §5). Blindness prevents the classifier from
grading Haiku's output more harshly than Opus's for identical code. Its output
is advisory data, not a gate — it never affects any exit code.

## 7. The pilot (AUDIT-1's deliverable) + human calibration

The pilot is a **thin slice**, not the matrix, chosen to exercise every code
path and every stratum at least once while keeping spend and memory bounded:

- **Tasks** — ~2 per stratum (≈10 tasks), each anchored to a `lang-spec/` ID.
- **Models** — the two extremes first (`haiku-4.5`, `opus-4.8`): the strength
  axis is most informative at its ends, and two tiers prove the axis works
  before paying for four.
- **Rungs** — L1 and L2 (the headline and its first diagnostic step).
- **Arms** — one-shot for the pilot; the iterative arm is validated on a single
  task before AUDIT-2 turns it on broadly.
- **N** — small (e.g. 3) per cell, enough to see whether a cell is flat (0/3 or
  3/3, prune it) or variable (spend more N in AUDIT-2). The pilot's job is to
  *estimate* the `N` the full sweep needs, per the adaptive-sampling discipline
  (methodology §5).

Concurrency is deliberately capped low in the pilot (a handful of probes in
flight, not the whole slice at once) so the run is resource-bounded and
interruption-safe — every completed record is durably written before the next
run starts, so a restart resumes from the JSONL, losing at most one run.

**Human-calibration hook (methodology §10.B — guardrail on the centerpiece).**
The pilot's confirmed LLM misses are packaged as a small, self-contained
newcomer task set (task + rung docs, no answer) suitable for a handful of human
volunteers. AUDIT-1 *produces the package and the protocol*; running it against
live newcomers validates that an LLM miss proxies a human miss before AUDIT-2
trusts the intuition sub-score. Until calibrated, every LLM miss is a
surprise-*candidate*, recorded as such, not ground truth.

## 8. Where the artifacts live

- **Design + task bank + oracle wiring** → tracked (`docs/audit/`, and the task
  fixtures alongside their expected-output sidecars). These are durable,
  spec-anchored, review-worthy artifacts — the audit is a publishable readiness
  study, and its instrument is part of the record.
- **Raw per-run transcripts + submissions** → out-of-tree (the scratchpad /
  a gitignored `audit-runs/`), like a build cache: voluminous, reproducible,
  not review material. Referenced from records by path.
- **The distilled scoreboard** → tracked, mirroring how `examples/STATUS.md` is
  a committed conformance snapshot. The scoreboard is the finding; the raw runs
  are its provenance.

This split keeps the review surface small (design + tasks + scoreboard) while
preserving full provenance for anyone re-deriving a number.

## 9. Staged epoch sequence

AUDIT-1 lands as a small PR chain, each independently reviewable, so the
instrument is built and de-risked before any large spend:

1. **Harness design** (this document) — the instrument spec. No runs.
2. **Task bank** — the stratified tasks anchored to `lang-spec/` IDs, each with
   its expected-output sidecar and reference-language statement
   (`methodology.md §9`).
3. **Oracle + runner glue** — the deterministic compile/run wrapper over the
   corpus machinery, plus the record-writer; validated on the task bank with a
   *known-good* submission (no subagent yet).
4. **Pilot** — the thin slice (§7) actually run through subagents, producing the
   first scoreboard and the human-calibration package; the report that sizes
   AUDIT-2.

Each step is behaviour-preserving on the compiler and gated by the usual rules
(build/test/vet/gofmt green; corpus ratchet untouched, since no example moves).
