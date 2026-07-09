# audit-runner — the deterministic audit oracle

The compile/run/compare half of the readiness-audit harness
(`docs/audit/harness.md §4`), written in Aril — it dogfoods the language exactly
as `tools/corpus-status` does, and reuses the same vendored `corpusexec`
subprocess adapter.

Given a task (`audit/tasks/<stratum>/<id>/`) and a submission `.aril` file, it
compiles the submission, runs it in the task directory, and compares stdout to
the task's `expected.txt` sidecar — emitting one record in the harness §1 schema.
It produces **only** the deterministic judgments `compile_ok` and `run_ok`; the
error-taxonomy and idiom-divergence classifications are a separate *blind*
strong-model pass (harness §6), so `error_class` is left empty here.

## Build & run

```
aril build -o audit-runner audit/runner
```

Two modes:

```
audit-runner                                   # validate (default)
audit-runner grade <task-dir> <submission.aril>
```

- **`validate`** — grade every task's known-good `reference.aril` and assert each
  is green. A red reference means the task bank is broken (a fixture rotted, or
  the compiler regressed) — a **tooling failure**, exit 1, never silent. It
  prints one JSONL record per task to stdout (proving the serialize path) and a
  final `#`-prefixed summary line; the exit code is the gate.
- **`grade <task-dir> <submission.aril>`** — emit exactly one JSONL record for
  one submission. This is the primitive the pilot (`harness.md §9` item 4) calls
  per probe output: the probe writes a submission, `grade` scores it.

The tool `chdir`s to the git root, builds the `aril` compiler once, and grades
each submission with `--contracts=panic` (a stated contract becomes part of the
behavioural check; inert when the submission has none) — matching the corpus
run-oracle.

## What it deliberately does not do

- **No subagents.** This PR is the deterministic oracle only; driving probe
  subagents is the pilot's job (harness §3/§9).
- **No error classification.** `error_class` / `idiom_divergence` are the blind
  classifier's output (harness §6), filled by a later pass — not guessed here.
  (`validate` stamps `idiom_divergence = "intended"` only for the reference,
  where it is ground truth, not a judgment.)

## Follow-up

Wiring `audit-runner validate` into CI would turn fixture-rot into a red build (a
task-bank ratchet parallel to the corpus one). Deferred to keep this PR to the
tool itself; the exit-code contract is already CI-ready.
