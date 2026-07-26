# Aril readiness audit — AUDIT-4 synthesis & verdict

The closing epoch of the readiness-audit arc. AUDIT-0 calibrated the docs and
wrote the L1 cheatsheet; AUDIT-1 built the subagent-probe instrument; AUDIT-2 ran
the intuition sweep (4 models × 3 rungs × 10 tasks) and found `compile == run` in
every curated cell; AUDIT-3 adversarially hunted the silent quadrant the curated
sweep structurally could not reach, and produced the trap catalog (T1–T19). A
remediation track (R1–R5) then turned the bulk of that catalog into compiler
changes. **This document synthesizes the whole arc, records the disposition of
every finding, and issues a readiness verdict for the v1 surface.**

The governing question the audit exists to answer: **does the language land in a
newcomer's intuition — does the syntax lie?** A green corpus proves *we* can
write the Aril we intended; it says nothing about a developer arriving with
TypeScript / Go / Rust priors. AUDIT-4 answers with data in hand.

## The arc, and what remediation discharged

AUDIT-3's central finding was that the silent-lie surface is real but **narrow
and mechanical**: Go's lowering leaks through wherever a value is *rendered*, a
container/field is *defaulted*, or a binding is *captured* — three boundaries, not
a pervasive rot. The remediation track closed them:

| AUDIT-3 finding | Class | Disposition | Where |
|---|---|---|---|
| T1 composite `%v` leak | silent-lie (flagship) | **fixed** — generated `fmt.Stringer` per composite | R3 (D56) |
| T2 omitted container field → nil | silent-lie / soundness | **fixed** — empty-container default; bare class field → E0220 | R1 |
| T3 `spawn` captures a `var` → race | silent-lie (concurrency) | **`-race` mode** (detectable) + gotchas note; capture-discipline deferred | R4; below |
| T4/T6–T12/T14/T16–T19 | honest-difference | **docs** — `gotchas.md` + cheatsheet trap rows | R4 |
| T5 Map/Set order vs spec | doc-vs-impl | **fixed** — spec committed to insertion order | R4 (D17) |
| T13 bare `Map[k]` zero on miss | honest-diff / design-Q | **keep + hint** (H0002); container sub-case defaulted | R1; this epoch |
| T16 discarded `Result` | silent-lie vs positioning | **hint** (H0001, the must-use tier) | this epoch |
| compiler bugs (unused-var, shadow leak, uninit crash, range-over-field, panic-line drift) | D10 leaks / crash | **fixed** — E0221/E0222, E0220, bug#4; `//line` drift deferred | R1/R2 |
| Finding #1 records nominal vs D14 structural | doc-vs-decision | **D14 amended** — records nominal for v1 | this epoch |

## The hint tier (D58) — the one net-new capability

AUDIT-4's build deliverable is the **`hint`** severity: a soft, non-blocking
teaching note for *spec-valid* code with a surprising or easy-to-misuse
behaviour. A hint renders with a `hint[Hxxxx]:` prefix, **never** fails the build
or changes the exit code, is default-on, and is suppressible globally
(`--hints=off` / `ARIL_HINTS=off`) or per-code (`ARIL_HINTS=off:H0001`). It is the
audit's soft *make-it-loud* channel: stricter to earn than a docs row, gentler
than an error. Forcing a spec-valid surprise to a hard error is a language-design
call, not a hint — the compiler *teaches*, it does not *complain*.

Two hints ship on it:

- **H0001 — discarded `Result` (T16).** A statement-position expression (or a
  unit-returning body's trailing expression) that drops a `Result` hints that the
  error path is silently ignored. `try` / `catch` / `match` unwrap it (exempt);
  `let _ = e` is the deliberate-discard escape hatch.
- **H0002 — bare `m[k]` on a class-valued map (T13).** A bare read whose value
  has no safe default (a class, or a record/tuple holding one) returns a nil
  pointer on a miss — a SIGSEGV landmine. The hint points at `m.get(k):
  Option<V>`. It mirrors codegen's defaulting exactly: container-valued misses
  default to empty and scalars/`Option` zero safely, so only the nil-landmine
  case fires.

The `sort.sorted` discoverability finding (AUDIT-2 F-sort) needed no hint-tier
work — `sort.Slice` is a *compile error* (unbound member), already carrying a
tailored E0217 suggestion, so it lives on the error tier, not the hint tier.

## The redesign(→D11) calls, resolved

The audit's principle — *traps default to docs/hint, not elimination; only a
genuine language-design change escalates to a redesign* — guided each surviving
call to its least-disruptive honest disposition:

- **T13 bare `Map[k]` — keep + hint.** Removing the bare index would be a corpus
  migration and a breaking pre-v1 change for a difference that is honest (it
  matches Go). Kept; the nil-landmine sub-case (a class value) is now taught by
  H0002, and `.get(k): Option<V>` is the safe form.
- **T3 `spawn` capture — docs + `-race`, discipline deferred.** A hint on a
  mutated captured `var` was prototyped (H0003) and **dropped**: it
  false-positives on safe idioms already in the corpus — a mutation guarded by a
  `sync.Mutex`, and a single-writer-per-var pattern made safe by the `scope`
  join. A static "captured var mutated in `spawn`" signal cannot separate these
  from a real race without the flow analysis that the concurrency roadmap
  (cross-channel trace-runtime) owns. A hint that cries wolf on correct mutex
  code is worse than none, so T3 rests on the shipped `-race` mode + the gotchas
  note; real capture-discipline is deferred to the CONCURRENCY-CORRECTNESS epoch.
- **Finding #1 records — D14 amended to nominal-for-v1.** The compiler types
  records nominally in v1 (two same-shape named records are not interchangeable;
  anonymous literals are written in named form), while D14 as written promised
  structural. The surface docs were already calibrated to the nominal reality in
  AUDIT-0; the decision record is now amended to match, with structural record
  typing deferred post-v1. The docs and the decision no longer contradict.

## Verdict — does the syntax lie?

**The governing invariant holds at the type boundary and, after remediation, at
the value boundaries where it used to break.**

- Where Aril is *strict and loud*, it is often **safer** than the priors it
  borrows from: insertion-ordered deterministic containers, per-iteration loop
  capture, `float64 = 5/2` rejected at compile time, exhaustive matching,
  `Option`/`Result` instead of null and exceptions, explicit interface
  conformance. These are the reassuring corners — the surface tells the truth and
  the truth is good.
- Where it used to *silently lie* — composite rendering (T1), container/field
  defaulting (T2), Map-order-vs-spec (T5) — the remediation track fixed the
  mechanism, not the symptom: one generated `String()`, one defaulting rule, one
  spec reconciliation each collapsed a whole cluster.
- Where a difference is *honest* (integer division truncates, floats print `%g`,
  `defer` is function-scoped, `unwrapOr` is eager) it is documented in one place
  (`gotchas.md`) and, for the two sharpest must-use cases, taught just-in-time by
  a hint.

The residual silent-lie surface is small, mechanical, and either fixed or
docs/hint-managed. **On the intuition axis, the v1 surface is ready:** a
TypeScript/Go/Rust developer's priors transfer or fail *loudly*, and the handful
of honest differences are catalogued and, at the two highest-frequency points,
surfaced by the compiler itself.

**The one dimension that is not a language question remains open: cold start.**
The engine to consume Go's ecosystem is built (external modules + module-aware
bindgen — the dependency system and generic binding path both resolve
end-to-end), but adoption — which libraries to bind first, driving a real hard
binding such as `database/sql` + a third-party driver all the way to a live
example — is work, not mechanism. That is the go-to-market risk the audit always
named as orthogonal to intuition, and it is where the roadmap points next.

**Readiness verdict: the v1 *language surface* passes the readiness audit.** The
syntax does not lie in a way that a newcomer hits silently and unrecoverably; the
exceptions are honest, documented, and — at the two sharpest points — taught by a
non-blocking hint. The remaining distance to real-world use is ecosystem breadth,
not surface correctness.
