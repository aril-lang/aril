# `audit/` — the readiness-audit task bank

The instrument's tasks. The **why/how** of the audit lives in `docs/audit/`
(`methodology.md` = what to measure and why; `harness.md` = the runnable
instrument spec). This directory holds the **task bank**: the concrete
programming tasks the probe subagents are asked to write, stratified and
anchored to the language spec.

This tree is **outside** the measured corpus globs (`examples/`, `user_tests/`),
so the conformance scoreboard never scores it — these are audit inputs, not
acceptance examples.

## Layout

```
audit/tasks/<stratum>/<id>/
  task.toml            # metadata: id, stratum, spec anchors, oracle mode, controls
  prompt.md            # the task statement handed to the probe (language-neutral)
  reference.aril       # a known-good Aril solution — NEVER shown to the probe
  expected.txt         # the oracle target (stdout), captured from reference.aril
  controls/            # reference-language statements (methodology §5 control)
    <id>.go
    <id>.rs
    <id>.ts
```

### Strata (methodology §9 + §12)

| stratum | what it probes |
|---|---|
| `prior-aligned` | leans on TS/Go/Rust priors; *should* be easy — a check that intuition wasn't broken |
| `aril-novel` | sum types, exhaustive `match`, `Result`/`try`, uncolored concurrency, contracts — where value and risk live |
| `trap` | looks like a TS/Go idiom but must not behave like one |
| `library-lookup` | needs a module API; grades the self-service protocol, never intuition |
| `contract-authoring` | write the contract, not just the code (the agent-facing differentiator) |

## The files, and the rule that makes the measurement valid

- **`prompt.md`** is **language-neutral**: it states the problem and the exact
  I/O contract, and it must **not** reveal Aril syntax or name the mechanism
  (say "report division by zero as an error", not "return a `Result`"). The same
  prompt drives the Aril run and every control-language run; what the probe
  reaches for is the measurement.
- **`reference.aril` is never part of a probe's context.** The harness builds a
  probe prompt from the rung documents + `prompt.md` only (`harness.md §3`). The
  reference solution exists to (a) *generate* `expected.txt`, (b) let the
  oracle-glue PR validate itself on a known-good submission before any subagent
  runs, and (c) document the *intended idiom* for the `idiom_divergence` grade.
  A probe that could read `reference.aril` would be measuring copying, not
  intuition.
- **`expected.txt`** is the exact stdout of `reference.aril`. Because these
  tasks print language-independent output, the *same* `expected.txt` is the
  oracle target for the Aril submission and for all control languages.

## `task.toml`

```toml
id       = "fizzbuzz"
stratum  = "prior-aligned"
anchors  = ["T-For", "T-Arith", "T-Cmp"]   # lang-spec artifact IDs exercised
oracle   = "stdout-exact"                    # compare stdout byte-exact to expected.txt
args     = []                                # CLI args to the built program

prompt    = "prompt.md"
reference = "reference.aril"
expected  = "expected.txt"

[controls]                                   # methodology §5 reference control
go   = "controls/fizzbuzz.go"
rust = "controls/fizzbuzz.rs"
ts   = "controls/fizzbuzz.ts"
```

## Validation status of the seed

Every seed task's `reference.aril` is compiled + run (`aril run`) and its
`expected.txt` captured from that run. The Go and Rust controls are compiled +
run and confirmed byte-identical to `expected.txt`. The **TS control is a
statement only** — no Node/`tsc` toolchain is present in the current
environment, so it is written to the same contract but not machine-validated
here; the oracle-glue PR wires TS validation where a toolchain exists.

## Growth

The seed proves the format end-to-end with two verified tasks (`prior-aligned`,
`aril-novel`). The bank grows to ~2 per stratum (≈10 tasks) for the pilot
(`harness.md §7`), then broadens in the full sweep (AUDIT-2). Each added task
carries its spec anchors, so coverage stays deliberate rather than ad hoc.
