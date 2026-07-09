# Aril readiness audit — methodology

*Working draft. Expect churn.* The compiler-capability corpus is fully green;
this document defines how we measure a different, orthogonal question: does the
language **land in a newcomer's intuition**?

> **References policy.** This methodology and every audit-result document reference
> only public project surfaces — the docs (`docs/`), RFCs (`docs/rfcs/`), the
> example/test **corpus** (`examples/`, `tests/`), and the **code** (`internal/`,
> `cmd/`), plus opaque decision labels like `(D14)` — so every link a reader follows
> resolves within the published project.

## 1. The invariant under test

Aril's governing design invariant is:

> **The syntax must not lie.** A construct that looks like a familiar idiom
> must mean what that idiom means; a novel construct must be guessable from
> its shape.

A green corpus proves *we* can write the Aril we intended. It says nothing
about whether someone arriving with TypeScript / Go / Rust intuition writes
correct Aril. Those are different claims, and the second is the whole subject
of this audit. **The audit is the empirical test of the invariant above.**

## 2. The instrument: subagent-as-intuition-probe

We use LLM subagents of varying capability, with controlled context, as
instruments that measure intuitiveness.

The methodological gift: models carry Go / TS / Rust in their weights but **not
Aril** (the language is new and post-cutoff). Their behaviour on Aril is
therefore *pure prior-transfer*. Where a model guesses wrong is exactly where
the language diverges from the intuition it advertises. The absence of Aril
from training is not a limitation — it is what makes the measurement clean.

**Code-agents are a first-class audience, not only a proxy (2026 design stance).**
Aril is designed on the premise that LLM code-agents are a *mandatory* target
surface — writing Aril well is something agents must do, alongside humans. That
gives the instrument **dual standing**: the subagent probes *directly measure* a
real target population (code-agents), not merely proxy human newcomers. The
human-newcomer proxy question (§10.B) still applies to the *human* half of the
audience and still needs calibration; but a low agent score is a first-class
defect in its own right, because agents are users. Design so the syntax lands for
both — and where the two audiences' intuitions diverge is itself a finding.

### Axes (independent variables)

| Axis | Values | What it isolates |
|---|---|---|
| **Model strength** | Haiku 4.5 · Sonnet 5 · Opus 4.8 · Fable 5 | reasoning budget — the weaker the model that succeeds, the more intuitive the construct |
| **Context rung** | L1 one-page cheatsheet · L2 language spec · L3 spec + worked examples · L4 spec + examples + compiler iteration | how much teaching a construct needs |
| **Task type** | TS/Go-prior-aligned · Aril-novel · trap · library-lookup | where surprise (and value) lives |
| **Feedback** | one-shot · iterative (compiler errors fed back) | learnability cost + diagnostic guidance |

There is **no L0 ("guess from nothing")**. Absolute self-evidence is not a
design goal. Intuitiveness is measured *relative to the minimal intended
teaching artifact* — the L1 cheatsheet is the floor of the ladder, not zero.

## 3. The headline metric: L1

The crispest readiness question is: **is one page enough to start writing
correct Aril?** That is the L1 pass/fail, and it is the number we report.

The other rungs are not equal steps — they are a **diagnostic descent** that
explains *why* an L1 failure happened. For each construct, the **rung of first
success × the weakest passing model** is a mechanical finding classifier:

| First cleared at | Meaning | Remediation bucket |
|---|---|---|
| **L1** (cheatsheet) | intuitive-at-minimum | — (goal met) |
| L1 fail → **L2** (spec) | the cheatsheet under-teaches | **docs / teach** |
| L2 fail → **L3** (examples) | prose insufficient; needs a shown idiom | **docs** (discoverability) |
| L3 fail → **L4** (iteration) | the shape is not guessable; only a dialogue with errors recovers it | **redesign / diagnose** |
| fails even L4 | missing feature or deep trap | **redesign / missing-feature** |

Because the classifier is mechanical, findings do not need hand-triage — the
rung and model that first succeed mark the bucket.

## 4. Two kinds of knowledge, two instruments

Not everything should be intuited. We split knowledge and grade guessing only
where guessing is fair:

- **Intuit-able** — syntax, semantics, control flow, the shape of constructs.
  Measured on the L1–L4 **content ladder**. Fair to grade "correct from the
  page."
- **Lookup-able** — *which modules exist* and *their exact API spelling*. This
  is reference material by nature; it cannot be derived from principles.
  Forcing an agent to guess an API is a category error. Measured not by
  guessing but by a **pointer → self-service protocol**: given a short
  description, did the agent (a) realise it must look something up, (b) find
  the reference, (c) read it, (d) call correctly?

Two gradeable self-service variants:

- **Pointer given** ("the API reference is *here*") → audits the reference's
  *legibility*: can correct calls be written from it?
- **Pointer withheld** ("a reference exists; find it") → audits *findability*:
  does the entry-point table / structure / cross-linking lead to it?

Consequently the L1 cheatsheet has a **dual job**: teach the language
(intuition) *and* point to the reference for the library (self-service). It is
**not** expected to carry the module/API list. The pointer-to-reference is a
mandatory element of the cheatsheet, and the audit tests it.

So the L1 headline decomposes into two sub-scores:

- **intuition-pass** — correct *language-level* code from the page;
- **self-service-pass** — from the page's pointer, found the module/API index
  and used it correctly.

## 5. Dependent variables and grading hygiene

Per run we record: compiles? · runs correctly (oracle)? · iterations-to-green
(iterative arm) · error taxonomy · idiom divergence (did it write it the
intended way, or an awkward workaround?).

**Error taxonomy with attribution routing.** The first-attempt error is
classified, and the classification *routes* it — this is the hygiene that keeps
the metrics honest:

- **syntax/semantics miss** → intuition axis (a real language/teaching finding).
- **API-lookup miss** → **never lowers the intuition score.** Routed by
  pointer availability:
  - no reference pointer was available → **not a finding** (unfair to grade).
  - pointer available, agent didn't follow it → **navigation** finding.
  - followed it, still wrong → **reference-legibility** finding (fix the
    reference, not the language).
- **missing-feature** → the agent reached for something Aril lacks (coverage
  signal).
- **prior-leakage / trap** → familiar idiom that compiled-but-diverges, or was
  rejected (see §6).
- **doc didn't cover / doc misled** → the context rung given was wrong or
  incomplete. The docs are on trial alongside the language.

**Reference-language control.** The same tasks are run in a language the models
*do* know (Go / TS / Rust) at the same rung. If a model writes correct Go
zero-shot but fails Aril, the gap is Aril's surprise surface, not task
difficulty. Without this control, "hard task" and "surprising language" are
indistinguishable.

**Statistical discipline.** N trials per cell; report rates with confidence
intervals, not single runs; pre-register the pass threshold; adaptive sampling
(spend trials where variance is high, not on cells that are trivially 100% /
0%). Grading is **blind**: a deterministic compile/run oracle for correctness,
and a separate strong-model classifier — which does not know which model
produced the code — for the error taxonomy.

## 6. Traps and the `hint` tier (remediation-design target)

Cross-language traps are **not** to be eliminated wholesale. Aril is not TS/Go;
honest divergence is legitimate. The goal is to **meet the newcomer with the
trap as early as possible**, so the rest of the journey is unclouded. A trap's
default remediation is therefore **docs (early onboarding)**, not redesign.

Two axes classify a trap:

| | **Loud** (compiler rejects) | **Silent** (compiles, means something else) |
|---|---|---|
| nature | safe, merely unfamiliar | dangerous — nothing caught it |
| remediation | doc note + diagnostic hint | a doc note is a *weak* guarantee here |

- An **honest difference** (Aril differs; just warn the newcomer) → an early
  doc note suffices.
- A **silent lie** (looks like idiom X, quietly does not-X, a reasonable person
  is burned) → escalate: prefer making it **loud** over documenting it. This is
  the one case where the "must not lie" invariant still bites. A documented
  *difference* is not a lie; a lie is a silent *surprise*.

### The `hint` severity tier

The loud form of an honest-but-surprising divergence is a **`hint` — explicitly
*not* a `warning`.** The industry trained everyone that `warning` = latent error
→ zero-warning policies, `-Werror`, CI gates. A trap-note delivered as a warning
pressures professionals to *eliminate* an inherent language difference
(impossible) or suppress the whole class (losing the onboarding value). So the
tier is named and framed differently:

- **Never affects build success or exit code.** A hint is a separate channel,
  not a soft error; it is invisible to CI gates *by construction*, which
  removes the source of zero-warning pressure.
- **The compiler teaches, it does not complain.** A hint is the gotchas note
  delivered just-in-time at the site you would trip — strictly better than a
  static page because it is contextual.
- **Default on, granular suppression by hint-code, global off via env**
  (`ARIL_HINTS=off`). Removing the training wheels is opt-*out*, not opt-in:
  the newcomer benefits and does not know to enable it; the professional who
  has internalised a trap silences that one code and keeps the rest.

Aril has a greenfield advantage: no inherited warning tier whose culture must
be fought. The tier can be introduced with the right framing from the start.

**Delivery is one content, many channels.** The static "coming from TS/Go/Rust"
gotchas page and the in-editor hint are the same content. Frequency (from the
sweep's prior-leakage cluster) decides the channel: a top trap earns a hint
*and* a line in the L1 cheatsheet; a mid trap → the gotchas page; a rare one →
a spec footnote. The trap catalog is therefore *generated from observed
behaviour and ranked by real damage*, not written from imagination.

**Sequencing.** The `hint` tier is a remediation-design decision, recorded here
as the **target form for the audit's trap escalations** — it is what "make the
trap loud" resolves to. The measurement epochs stay behaviour-preserving on the
compiler; the tier is built afterward. The audit *produces the evidence*: which
hints (frequency-ranked trap clusters) and their exact wording (from what
confused a model and what un-confused it in the iterative arm). A later
re-sweep validates the tier before/after — did adding the hint move the
intuition score on that cluster?

> **Prior-art note (§10.D), corrected 2026-07-09.** Elm attaches a hint to
> cross-language priors it *rejects* — but only because those are already
> ill-typed in Elm; that is **not** a licence to hard-reject *spec-valid* code
> to protect a newcomer (which is user-hostile and paternalistic — ordinary
> users are enraged when the compiler forbids code the spec allows). The lesson
> narrows to **also carry a habit-naming hint on genuine rejections**. The soft,
> non-blocking `hint` tier below **remains the primary tool** for
> spec-valid-but-surprising divergence; forcing such a construct to a hard error
> is a spec/redesign decision (D11), never a default. The talk evidence
> corroborates the soft tier: Kotlin's non-blocking "grey/green code" channel
> and Gleam's stress-reducing hint framing are exactly this (§11.1) — Elm's
> reject-hard is the outlier author-opinion, not the norm.

## 7. Remediation buckets

Every finding lands in exactly one:

- **teach / docs** — a documentation, cheatsheet, or hint-wording gap. Cheap.
- **diagnose** — the compiler should catch this confusion but is silent; grow
  the diagnostic surface (a `hint`, or a hard error for a genuine mistake).
- **redesign** — the surface itself misleads; an open syntax question (D11).
  Expensive; the language-owner's call. Silent lies default here.
- **missing-feature** — the construct the agent reached for does not exist.

**Measurement precedes remediation.** The measurement epochs are
behaviour-preserving on the compiler, so the scoreboard is measured against a
fixed target. Remediation is batched afterward; otherwise the score chases a
moving target.

## 8. Reuse of existing machinery

The audit generalises and instruments existing capability rather than
reinventing it:

- **Agent-authored corpus (D35)** — subagents already implement tasks on the
  real compiler and return an error→fix log. The audit adds *controlled*
  model/context and captures the trajectories as data.
- **Run oracle (D25/D34)** — expected output / ordered-subsequence patterns are
  the correctness grader.
- **`diag_ok`** — measures message *ideality* against a sidecar. The audit's
  diagnostic-recovery arm measures the *consumer* side (did a confused model
  recover). Complementary; the audit populates new sidecars from real
  confusion.
- **Formal artifact inventory (D17)** — every keyword / production / diagnostic
  has an atomic fixture. That inventory is the principled skeleton of the task
  bank, so coverage is not ad hoc.

## 9. The task bank (skeleton)

Anchored to the `lang-spec/` artifact IDs, stratified so the surface is covered
deliberately:

- **prior-aligned** — leans on TS/Go/Rust priors; *should* be easy. A check
  that we did not break intuition.
- **Aril-novel** — sum types, exhaustive match, `Result`/`try`/`catch`,
  uncolored concurrency + `scope`, contracts. Where both the value and the risk
  live.
- **trap** — looks like a TS/Go idiom but should not behave like one. Feeds §6.
- **library-lookup** — requires a module API; grades the §4 self-service
  protocol, never intuition.

## 10. Prior-art calibration (research-validated deltas)

A cross-language retrospective sweep (Elm, Gleam, Borgo, ReScript, Rust editions,
plus empirical PL-adoption and LLM-proxy studies; 24 of 25 extracted claims
survived 3-vote adversarial verification, 1 refuted) produced the following
deltas, ranked by leverage on this plan.

**Read these as calibrated evidence, not law.** Several are a single language
team's design *opinion* (Elm's assistant philosophy, Gleam's minimalism), not
validated user preference — apply the same skepticism the LLM-proxy caveat (B)
demands: an author's design-preference is a **hypothesis to test against Aril's
own probes and a human sample**, not a norm to adopt. Weight peer-reviewed /
empirical findings (OOPSLA'13, the LLM studies) above author blog-opinion.
Concretely: hard-rejecting *spec-valid* code because one team prefers it is
user-hostile and out of scope (see §10.D) — the audit surfaces the mismatch; it
does not adopt the remedy by authority.

**A. Cold-start is a co-headline dimension, not a byproduct (highest leverage).**
OOPSLA'13 (Meyerovich & Rabkin, 200k+ projects) found available open-source
libraries the single most influential adoption factor, while intrinsic
simplicity/safety rank near the bottom ("Simplicity will not attract many
programmers"). Borgo — the closest analog (compiles to Go) — shows "compiles to
Go ≠ ecosystem available": full Go compatibility still needed hand-written
declaration files and only a small stdlib subset was bound. **Change:** add an
**ecosystem-readiness dimension co-equal with intuition** — measure how much of
the Go stdlib is actually bound and usable, how much a missing binding gates a
real task, and how easily a newcomer binds what is missing. The §4 self-service
sub-score gains weight. (Aril already has bind-not-port + module-aware bindgen;
the audit measures *coverage and the binding burden*, not the mechanism.)
Sources: OOPSLA'13 (lmeyerov.github.io/projects/socioplt/papers/oopsla2013.pdf);
Borgo (github.com/borgo-lang/borgo).

**B. The LLM-probe is unvalidated as a human proxy — calibrate, do not trust
blind (guardrail on the centerpiece instrument).** No source validates that LLM
priors faithfully proxy human-newcomer priors; two human studies caution:
beginners and code-LLMs systematically "misread each other" (CHI'24, 120
novices), and GPT-4-generated error messages beat conventional compiler messages
in only 1 of 6 tasks (106 novices). **Change:** add a **human-calibration step
in AUDIT-1** — validate LLM wrong-guesses against a small human-newcomer sample
before trusting the intuition sub-score; treat every LLM miss as a
surprise-*candidate* to investigate, not ground-truth intuition. Sources:
arXiv 2401.15232; arXiv 2409.18661.

**C. Diagnostics are first-class UX and a design constraint (Elm, Gleam).** Elm
reframes the compiler as an assistant (shows source-as-written, attaches a
concrete hint to every error). Gleam lets "confusing error messages" veto a
whole feature (type classes are "not planned"), keeps a fault-tolerant compiler
that analyses broken code, and lists missing exhaustive-match patterns in
*definition order*, not lexicographically. **Change:** add a
**diagnostic-ergonomics measure** — does an Aril error show source-as-written +
a concrete hint; do exhaustive-match diagnostics list missing variants in
definition/source order; does tooling degrade gracefully on broken code. Add an
**AUDIT-4 criterion:** a feature whose diagnostics cannot be made intuitive is an
exclusion candidate (minimalism as a learnability lever). Sources:
elm-lang.org/news/compilers-as-assistants and /compiler-errors-for-humans;
gleam.run/frequently-asked-questions, /news/the-happy-holidays-2025-release.

**D. `hint`-on-a-rejection — but the tier is NOT demoted (corrected 2026-07-09).**
Elm attaches a beginner hint to cross-language priors it *rejects* (JS `+` for
concat, "truthy" conditionals). The narrow, legitimate lesson: **also attach a
friendly, habit-naming hint to genuine rejections** — because those constructs
are *already ill-typed* in Elm, the hint merely softens an existing rejection.
It does **not** license hard-rejecting *spec-valid* code to protect a newcomer:
that is user-hostile and paternalistic (ordinary users are enraged when the
compiler forbids code the spec allows), and Elm's preference for elimination is
one design opinion, not validated user preference. So the soft, non-blocking
`hint` tier (§6) **stays the primary tool** for spec-valid-but-surprising
divergence; escalating a valid construct to a hard error is a spec/redesign
decision (D11, the owner's call), never a default audit remediation. The talk
evidence corroborates the soft tier over Elm's outlier reject-hard (§11.1).
Source: elm-lang.org/news/compilers-as-assistants (author opinion — treat as a
hypothesis, per the note above).

**E. Contrast-first onboarding + a live surprise-catalog (operationalizes
measurement-first).** The canonical "for X programmers" guides (r4cppp) and
Gleam's "for Elixir users" cheatsheet teach only the *differences* and annotate
divergence points inline (equality/comparison type rules). Elm drove most
error-message improvements from a public catalog of real broken programs.
**Change:** structure the L1 cheatsheet as **contrast-first** ("coming from
TypeScript/Go/Rust") with a "compiles-but-differs-from-TS" annotation column;
make the intuition sub-score test difference-points specifically; build a
**persistent, frequency-ranked surprise-catalog** from the probe wrong-guesses
that drives remediation. Sources: github.com/nrc/r4cppp;
gleam.run/cheatsheets/gleam-for-elixir-users; github.com/elm/error-message-catalog.

**F. Positioning caveat (scope honesty).** OOPSLA'13: developer movement between
languages is driven by domain/ecosystem, not syntactic similarity — a
TS-familiar surface is an *onboarding-ease* lever once a developer chooses Aril,
not an *acquisition* lever. This audit measures onboarding-ease / intuition, not
adoption; passing it is not adoption. Forward-looking (low priority): define
Aril's evolution/versioning story early — Rust editions support all editions
simultaneously and never split the ecosystem; ReScript's rename fracture is the
cautionary counterweight (Aril already carries `edition`/`min-aril` in the
manifest). Sources: OOPSLA'13; Rust RFC 2052.

**Gaps the sweep could not close** (no primary claim survived verification):
ReScript's BuckleScript→ReScript rename fracture, Scala's complexity backlash /
Scala 3 migration, and Kotlin's Java-interop cold-start. These are the target of
a complementary conference-talk transcript pass (Odersky / ReScript / Kotlin) —
§11.

## 11. Conference-talk deltas (primary-source talks)

Seven author talks were transcript-mined (Borgo/Pellini, Kotlin/Breslav,
ReScript-BuckleScript, Scala/Odersky ×2, Elm/Czaplicki ×2, Gleam/Pilfold,
Roc/Feldman) for what they add beyond §10. Grouped by where they change the plan.
Read them under the §10 rule: an author's preference is a hypothesis, not law.

**11.1 The soft `hint` tier is vindicated (not demoted — reinforces the correction to §10.D).**
Kotlin ships a non-blocking "grey code / green code" channel for
redundant/non-idiomatic code, orthogonal to error/warning; Gleam frames hints as
a stress-reducing, impostor-syndrome-aware culture nudge that carries the fix-list
("go to these files"), not a complaint. Both are exactly the soft tier — Elm's
reject-hard is the outlier. **Reframe:** the tier flags *redundant ceremony the
type system already knows is unnecessary* (a redundant cast, a defensive `else` on
an exhaustive match), not only cross-language traps.

**11.2 New metric — idiom-conformance ("works-but-wrong-idiom").** Kotlin's whole
thesis is "get out of your Java habits": Java-shaped code *runs*; the failure is
idiom-divergence no compile error catches. **Add** a sub-metric distinct from
compile/run correctness — how far newcomer Aril drifts from idiomatic Aril even
when it type-checks; the hint tier (11.1) is its remediation.

**11.3 "Simple ≠ easy" splits the headline (Scala).** *Easy* = familiar; *simple*
= composes cleanly — orthogonal. Aril's TS flavor buys *easy*. The highest-value
failure is a probe that passes because it *looked* like TS but the newcomer was
**confidently wrong** — easy masquerading as simple. **Add** a "name the core"
probe (one page → ask the subagent to reconstruct Aril's essence); Scala's
"kitchen-sink" reputation was a *perceived*-core-size failure independent of
actual size.

**11.4 Minimalism levers, sharpened (Scala, Gleam).**
- **Redundancy count** — "N ways to do X" is the #1 minimalism metric; penalise a
  valid-but-non-idiomatic second spelling (Scala 3 carrying implicits *and* givens
  doubled the surface; one page can't teach two systems).
- **Composition-scaling** — a feature whose cost is *superlinear* as usage grows
  (Scala implicits) is an exclusion candidate; *quarantine* behind an opt-in flag
  is a middle path but still enlarges the language you must *read*.
- **Escape-hatch exclusion (Gleam)** — a feature is an exclusion candidate if it is
  research-hard AND a bound-Go escape hatch covers it acceptably (Gleam excluded
  typed OTP: ~95% don't need it, untyped Erlang covers the rest). **Correction:** the
  Gleam talk does *not* evidence "diagnostics veto a feature" (§10.C) — demote that
  citation; the real, better-evidenced Gleam lever is minimalism-via-escape-hatch.

**11.5 Cold-start = effective surface, not spec surface (Scala, Kotlin, Borgo, Gleam, Roc).**
- **Effective surface** = base + de-facto library stack (Scala's "Scala + cats/ZIO/
  FS2" two-level tax). Test whether a real task (HTTP/SQL/files) needs only bound Go
  stdlib as on the cheatsheet, *without* a bespoke wrapper vocabulary.
- **Binding = minimal core + extensions** — Kotlin String is 5 methods + extension
  functions over a type you don't control = the exact bound-Go-type move. Self-service
  tests whether a newcomer extends a bound Go type rather than expecting a fat API.
- **Quantify coverage** — Borgo markets per-call demos ((T,bool)→Option, (T,*error)→
  Result) but is silent on breadth. Add a quantified binding-surface metric + a
  catalogue of Go signature shapes *outside* the auto-convertible set (3-value returns,
  meaningful `(n int, err error)`, callbacks, channels).
- **Bidirectional citizenship (Gleam)** — the cold-start test is not "can Aril call Go"
  but "can a Go team consume Aril output idiomatically + reuse `go get`/tooling
  unchanged." Add a dimension: does generated Go read/behave like hand-written Go
  (names, error conventions) — while keeping Go-as-IR.
- **Interop is the advertised escape hatch (Roc)** — the cheatsheet must answer "what
  if the binding isn't there?" → "drop to Go"; probe its *discoverability*.

**11.6 "Syntax must not lie" — four hard cases + probes.**
- **Semantics-leak (Borgo "gosm": Rust syntax, Go semantics)** — probe where Go
  semantics leak through the TS-familiar surface: zero-values, `defer`, mutexes,
  structural interfaces, numeric coercion.
- **Divergence-through-the-backend (ReScript)** — Aril lowers to Go and may exploit
  Go-native ops (int→string, map, equality); the source can silently diverge in the
  *IR*, not the surface. Probe: newcomer predicts a numeric/string/equality edge;
  Aril must honor *source* semantics, not Go's.
- **Simplified-surface-vs-honest-signature (Scala `map`/CanBuildFrom)** — a binding
  *may* present a simplified newcomer type **iff** it is a strict, non-surprising
  specialization (the cheatsheet type never permits/forbids what the real one
  doesn't). Test: predict behaviour at a boundary (empty collection, type-changing
  map) from the cheatsheet type alone; a mismatch is the *lie* the invariant forbids.
- **Name-honesty (Roc "unsafe in the title")** — ops that can fail/block/panic carry
  it in the spelling; probe whether a newcomer predicts fail/block/panic from the
  spelling alone.

**11.7 Cheatsheet design (Elm, Kotlin, Scala).**
- **Layered in concept-dependency order**, not alphabetical (Elm: good design
  communicates its own use; "builds on the previous stuff").
- **Contrast table** "in TS you'd write X → in Aril write Y, because Z" is a *named,
  user-demanded* artifact (a Java dev on the Scala talk asked for exactly this
  Java→Scala idiom map and "didn't find enough"). Probe variant: give a TS idiom, ask
  for the Aril idiom — tests trap + self-service jointly.
- **Active transform** (Kotlin's J2K "paste Java → get Kotlin") — consider a "paste
  TS/Go → idiomatic Aril" transform as a cold-start artifact; measure whether it beats
  the static page.
- **Framing** — avoid "beginner-friendly"; the honest claim is "self-teaching / the
  syntax doesn't lie" (Elm). Keep config in a familiar format (TOML/JSON), not a
  bespoke DSL (Gleam: the Nix barrier).

**11.8 Positioning & governance — risks the plan under-weights (Elm ×2; sharpens §10.F).**
- **Success ≠ adoption metrics** — onboarding-ease is a distinct axis that trades off
  against adoption/ecosystem-size; passing the audit predicts neither. State it.
- **Surface churn shelf-life** — a moving surface undermines a stable cheatsheet;
  snapshot the compiler version per run; treat churn as a variable, not a constant.
- **Intuition-signal ≠ change-mandate** — separate "a newcomer expects X" (valid
  signal) from "Aril should change to X" (a decision with context the newcomer lacks);
  don't auto-translate surprise into a change backlog (Elm's "why don't you just…").
- **Scope-discipline bias / obligation pressure** — an onboarding audit structurally
  biases toward "add the familiar feature", the exact pressure a small language must
  resist; classify every "missing feature X" as genuine-blocker vs scope-discipline,
  and prioritise findings (cheap+high-value) rather than dumping every stumble as an
  equal-weight defect (maintainer-burnout risk).
- **Feedback venue shapes toxicity** — capture findings in an intent-scoped,
  context-rich channel, not a bare comment thread.

**11.9 Validation methods (Scala, Kotlin, Roc).** Even Scala/Roc run on **taste +
user feedback, no formal usability studies** — so frame the LLM-probe as a *fast
usability-regression harness*, not a validated human-intuition predictor (reinforces
§10.B). Kotlin's IDE inspections (grey/green, J2K) are the closest thing to a
scalable continuous idiom-signal — model as Aril idiom-drift telemetry (feeds 11.2).

**11.10 Trust affordance — `aril emit` already exists (no new mode needed).** Three
analogs sell an inspectable lower form as a trust/cost-model lever (Borgo's WASM
playground, Kotlin's decompile-to-Java, ReScript's readable JS). Aril already ships
the affordance: **`aril emit <file.aril>`** prints the lowered Go to stdout
(`-no-line` strips the `//line` directives for a cleaner read). So this is **not** a
constraint-tension nor a build task — exposing the IR on demand does not make
readable-Go a *goal*. The only audit question left is **discoverability**: is a
newcomer pointed to `aril emit` as the "what did my code become / what does this
cost" trust aid? That is a cheatsheet/onboarding line, not an AUDIT-4 decision.

**Gap still open** (the transcript pass could *not* close it): the ReScript talk
predates the 2020 BuckleScript→ReScript rename, so the rename-fracture cautionary
tale remains unsourced — it needs a post-2020 ReScript-era retrospective. The talk
did surface the *pre-conditions* (three drifting identities: OCaml syntax / Reason /
BuckleScript) → keep {surface, toolchain, ecosystem} under one name; never expose a
newcomer to a second name for one thing.

## 12. Contracts as the agent-facing spec (a differentiator to audit)

Aril's contracts (RFC-0006 value pre/post/invariants + RFC-0007 channel trace
properties) are an *executable spec inside the language*. Classic Design-by-Contract
(Ada/SPARK, Eiffel) was **right in substance, wrong in timing**: human programmers
won't deliberately write contracts (ceremony without felt payoff), so it stayed
niche — the consumer motivated enough to pay the authoring cost and reap the
verification payoff did not yet exist. Ada tried; it missed the moment.

**Code-agents flip both sides of that economics — which is why it lands *now*:**
- *Authoring cost → ~0 for the agent.* An agent has no human aversion to ceremony;
  it emits a contract as readily as code. The artefact humans wouldn't write, agents
  will.
- *Payoff → immediate and structural.* A contract is simultaneously (a) a
  machine-checkable **target** the agent codes against, (b) a self-**oracle** that
  catches the agent's own mistake at the exact site (the RFC-0006 property: a firing
  contract indicts the code, in Aril coordinates), and (c) the **spec ⇄
  implementation bridge** — where "what was asked" meets "what the code does",
  checkable, not prose. The user's point: **the contract must correlate with the
  spec** (an agent's task spec becomes the code's contract).

So contracts-as-spec is precisely the surface *ordinary languages do not provide*,
and it is a core Aril differentiator **for the code-agent audience** specifically —
CodeSpeak-style spec languages and Aril's in-language contracts are two answers to
the same 2026 need.

**Audit consequence — a contract-authoring track (agent audience):**
- Given a task spec, can an agent express it as Aril *contracts*, not only code? Do
  the contracts it writes actually fire on a broken impl (the authoring rubric — no
  `x==x` trivia)?
- Spec → contract → code round-trip: the agent, given a spec, writes contracts, then
  code that satisfies them, and the contract catches its own errors — does the
  contract hold as the bridge?
- This is where Aril's differentiation over "ordinary languages" is *measurable* for
  agents: a high-value task-bank stratum (§9), distinct from plain compile/run
  correctness and from the human-intuition sweep.

**The flagship hypothesis (user, 2026-07-09) — contracts help *weak* agents most.**
Contracts *shift* cost: writing the code+contract is *harder*, but debugging it and
finding the error is *easier* (the oracle localizes the fault at its Aril-coordinate
site). The prediction is an **interaction**: the net benefit of contracts is *larger
for weaker models* — a weak agent flails without an oracle but converges with one.
Test it on the model-strength × context matrix, iterative arm, with two sub-conditions
that separate the hypothesis's two halves:
- **contract-provided** (the contract is given; the agent only writes code against it)
  *isolates the debug-benefit* — the clean test of "does the oracle help a weak agent
  converge," free of the authoring confound.
- **contract-authored** (the agent writes both) tests the full loop and exposes the
  authoring cost + the confound of a *wrong* contract (too weak → a false-negative
  oracle that passes bad code; too strong → false-positives that mislead).

Metrics — **authoring:** does the agent produce a *valid, firing* contract (fires on a
broken impl; no `x==x` trivia)? **debugging:** iterations-to-correct, final
correctness, and **fix-targeting** — did the contract's firing redirect the agent's
next edit to the indicted site? **The interaction — the contract-benefit widening as
the model weakens — is the result that would validate the differentiator.** A uniform
benefit, or "the weak agent can't author a usable contract so authoring-cost
dominates," refines or falsifies it; if contract-authored underperforms
contract-provided, the bottleneck is contract-*authoring*, not the concept. (Prior-art
angle for the research thread: does a machine-checkable oracle scaffold weak reasoners
more than strong ones — the "tests/contracts help weak coders most" pattern?)

**Authoring-guidance sufficiency — and a dedicated Aril-authoring *skill*.** The human
L1 cheatsheet has an agent-audience sibling: an **agent-targeted authoring artifact**
that encodes the *recommended* way to write Aril (idioms, contract authoring,
trap-avoidance, the Go escape hatch), ideally packaged as a dedicated, agent-consumable **skill**
(`skills/aril-authoring`).
The headline agent-audience question mirrors "is one page enough": **is the skill
sufficient to get an agent to *recommended-style* Aril** — not merely compiling Aril,
but idiomatic, contract-bearing, trap-free? A shortfall (idiom-drift, missing/weak
contracts, a trap the agent still hits) is a *teach* finding that improves the skill.
This dogfoods cleanly: the probe agents consume the very artifact under test, so each
revision is re-sweep-validated (before/after idiom-conformance, with the surprise-catalog
as the improvement backlog). AUDIT-0 drafts the skill alongside the cheatsheet;
iterating it to sufficiency is a running deliverable.

**A credible expert bets the opposite — the reconciliation sharpens the thesis
(Breslav / CodeSpeak).** Andrey Breslav's CodeSpeak (a spec language, his post-Kotlin
startup) bets *against* Aril on two axes: (1) *who authors the spec* — CodeSpeak has
**humans** write a *minimal* intent document and lets the LLM guess the rest;
machine-generated specs are an anti-pattern ("what is the purpose of a document
generated by a machine? people don't review them"). (2) *formalism* — CodeSpeak is
deliberately informal (closer to Python/JS than a typed language) because "the compiler
is an LLM that only speaks natural language," so static guarantees buy little. This is
the obvious "but a serious designer disagrees" challenge, and it has a clean answer the
audit must state: **the two operate at different layers.** CodeSpeak's artifact is
*human-readable prose consumed by the LLM upstream of code generation*; Aril's contracts
are *executable checks consumed by the compiler/runtime downstream of the LLM*.
Breslav's objections to formalism target the former and largely do not reach the latter
— Aril reaps machine-checkability *after* the LLM writes the code, sidestepping the "LLM
only speaks NL" objection. Record this as the named threat-to-validity Aril answers.
(Nuance he concedes: catching contradictions "at the level of intention" via a rule
system is desirable-but-hard — Aril's executable contracts are one concrete mechanism,
at the impl-conformance layer, not the intent-prose layer.)

**Task-bank difficulty axis — standardness / originality (Breslav, first-hand).**
His field observation: agents work well on standard, library-shaped concepts but
*forget/drop* features the moment a task needs a **custom abstraction** — until "normal
specifications" defining the abstraction fixed it. Plus his stated law: *human input ∝
the non-triviality of the task* (boilerplate needs ~none; an original program needs
much). So the value of a machine-checkable artifact (spec/contract) is near-zero on
boilerplate and **spikes precisely when the task leaves the model's built-in
vocabulary**. **Stratify the task bank by standardness / originality**, and expect the
contract-benefit — and the T2 weak-agent interaction — to be largest on the
novel-abstraction end: a clean, falsifiable curve. Related T1 asset: Aril's contracts
operationalize Breslav's own aspiration to *review behaviour, not code* (a behavioural
oracle over reading the code) — candidate audit metric: "can a reviewer accept a change
from contracts + test outcomes alone, without reading the code?"

**Breslav is complementary, not contradictory — and the synthesis sharpens Aril's core
bet (user, 2026-07-09; Pragmatic Engineer interview).** Breslav and Aril are not opposed
bets; they invest in *different halves of the same loop*. Breslav optimizes **authoring**
(a minimal human intent doc; a strong model guesses the rest, ideally correct in one
shot). Aril is more pessimistic about that one-shot: correct code is *not* written
first-try — verification is unavoidable, and **Breslav agrees** ("skip review → it
collapses in days"). The only question is verification's *form*. Informal verification
(code review) is necessary but opaque and
doesn't scale to agent volume. **Aril's bet: formal, executable contracts make
verification *transparent* — explicit, localized (Aril coordinates), legible as intent —
and transparent verification makes the whole development loop simpler and cheaper.** The
two stack: CodeSpeak on the authoring side, Aril on the verification side.

**The unified hypothesis (what Aril bets on).** The value of transparent formal
verification is *monotonic in the writer's error-proneness*: smallest in Breslav's
optimistic limit (strong agent, standard task, rich corpus, one-shot-correct), largest
exactly where reality lives. That folds three separately-surfaced findings into **one
measurable curve** — T2 (a weaker model), the corpus-gap (a *new* language the model
writes worse), and task novelty (custom abstractions off the model's built-in
vocabulary) are all axes of the *same* error-rate. **Measure verification-value against
all three at once** — model-strength × language-familiarity (Aril vs Go/TS) ×
task-standardness — and the prediction is a single rising surface. The "threats" below
(corpus-gap, design-for-humans) are its **boundary conditions**, not refutations: they
mark where the benefit is smallest, which the curve already predicts.

*Boundary conditions the audit must test head-on:*
- **The training-corpus gap (the biggest).** "Any LLM writes Python better than Rust or
  even Kotlin" — a *new* language has no corpus, so agents write it worse; Aril is
  brand-new *and* compiles to Go. **Direct test:** benchmark agent-authored Aril vs
  agent-authored Go/TS on the same tasks (an agent-audience reference-language delta) —
  quantify the writability penalty. Aril's answer is not "agents write Aril well" but
  **verifiability compensates for low writability** (the agent writes weak Aril, the
  oracle catches it — T1/T2); measure whether the contract/oracle layer closes the gap,
  a move Breslav never considers.
- **"Design for humans, not models."** Humans don't scale/improve; models do; so design
  for the binding constraint (the human). Rebuttal = T2 (the oracle's value is
  convergence/debugging leverage, persistent regardless of model strength, largest when
  the model is weak or the task hard). **Breslav offers no evidence for T2 — it is
  unsupported by this source; seek it in the CodeSpeak deep-research and the audit's own
  experiment.**

*Strong support for T1 + new metrics:*
- **The lost intent layer.** "I talk to the machine in human language but share only the
  machine language; my chat history is lost." A durable, committed, executable intent
  artifact is the industry's missing piece — Aril's checked-in contracts *are* that
  layer. A positioning asset.
- **Oracle-reviewability is the new bottleneck → metric "oracle legibility."** When an
  oracle replaces code review, the oracle becomes what humans must trust; long tests are
  hard to verify against intent, while a postcondition/invariant is shorter and more
  intent-shaped. **Measure whether a human confirms intent faster from an Aril contract
  than from the tests an agent would write** — a concrete edge over tests-as-oracle
  (CodeSpeak).
- **Contract conciseness + clean diffs → a quality bar.** His own R&D bar: the spec must
  be short relative to the code and *diff proportionally* to a small code change. Apply it
  to Aril contracts: take a corpus behavioural change, verify the contract delta is
  proportional; a contract layer that balloons/churns fails his test too.
- **Agent-facing tooling, not raw text → T3 presumes structured feedback.** Agents
  underuse code tooling (grep over an index → more tokens/time); for an agent to author
  Aril well, ship machine-consumable diagnostics (contract violations, type errors in
  Aril coordinates) as structured tool feedback, and measure token/iteration cost with vs
  without.
- **The agent-driving skill is real but not yet formalized.** Exactly the gap the Aril
  authoring skill fills — "skill sufficiency" tests something the wider field has *not*
  achieved (a differentiator if it works; a warning that success is prompt-dependent).
- **Verification relocated, not dropped.** "Just skip review — works for a couple of days,
  then it collapses." The pitch is not no-verification but verification moved from human
  reading to a machine oracle; the audit should show the oracle prevents that collapse.

*(Topic caveat: ~75% of the interview is Kotlin history; the AI-era material is one
~25-min segment. CodeSpeak vs Aril is a spec-language-vs-in-language-contracts
positioning contrast, distinct from the intuition/cold-start axes.)*

**External evidence (deep-research, 106 agents; 22/25 claims verified, 3 refuted) — the
thesis is supported, but the flagship hypothesis is unsettled and may run the wrong way.**

*Confirmed / supportive:*
- **CodeSpeak** is Breslav's 2025 venture — humans write NL/Markdown specs, the toolchain
  generates code ("maintain the spec, not the code"; CLI `impl`/`test`/`takeover`). Specs
  are NL intent, explicitly *not* machine-checkable contracts, and **humans** are the spec
  authors. It corroborates spec-as-first-class but does **not** occupy Aril's
  executable-contracts niche — and *no incumbent targets the agent as contract-author*,
  which is exactly Aril's novel claim (positioning, not head-to-head).
- **Agent-oriented design is real prior art, on two *separable* levers:** **constrained
  syntax** (Anka — a DSL enforcing one canonical form per operation, because GP-language
  syntactic flexibility causes systematic LLM errors: 99.9% parse, +40pp over Python) and
  **structured machine-readable diagnostics** (self-reflective APIs — an error as an
  actionable recovery payload, not prose; only the *qualitative* result survived, the
  magnitudes were refuted). → **two audit strata to measure separately from contracts:**
  (a) does Aril's syntax carry costly ambiguity (multiple valid forms) agents trip on?
  (b) are Aril's contract-violation/compiler diagnostics *agent-parseable and localized*,
  not merely human-readable?
- **DbC is agent-authorable *now*, but at a coin-flip rate.** Marmaragan (GPT-4o →
  SPARK/Ada contracts) verifies at **~50.7%** (36/71) — the strongest single datapoint for
  "DbC mistimed by Ada, landing now via agents", and a realistic ceiling: the
  contract-authoring probe should measure *success rate* against this ~50% baseline and ask
  whether Aril's ergonomics beat SPARK's. And "humans won't write contracts" is really an
  *adoption-cost* barrier, not a unicorn problem (in contract-adopting ecosystems >33% of
  elements carry contracts, stable) — so lowering the authoring cost via agents is exactly
  the lever that changes the economics.

*The flagship hypothesis (T2) — under-evidenced, and possibly the wrong sign:*
- Indirect support only: a compiler-as-oracle across 16 models (135M–70B) raised compile
  success 5.3–79.4pp with *gains inversely correlated with model size* ("tooling can
  substitute for scale"). **But a compiler is a *weak* oracle — it scaffolds compilability,
  not semantic correctness.** Aril's contracts target the *semantic* layer the compiler
  can't reach, so this evidence does **not** transfer automatically.
- **The three claims that would most directly support "oracle helps weak models more" were
  refuted (0-3),** and one *clean* datapoint runs the **other way**: Anka's weaker model
  (GPT-4o-mini, +26.7pp) gained *less* than the stronger one (+40pp); a self-reflective-API
  result found the weakest model gained *least* — it *could not exploit the structure*. **No
  study cleanly isolates the capability×oracle interaction.**
- **Consequence — this is priority, novel science, not a confirmation exercise.** The
  weak-agent test must (i) span a real capability gradient and measure *disproportionate*
  benefit explicitly; (ii) separate a **compile-level** oracle from a **semantic
  (contract-level)** one; (iii) separate **contract-provided** from **contract-authored**;
  and (iv) be designed to *detect a reversal* — the live, evidenced failure mode is that a
  weak model cannot author a valid contract or exploit the oracle, so the benefit *shrinks*
  with weakness. The §12 unified curve ("monotonic in error-proneness") is therefore the
  **bet, not a fact**; the crux open question is whether a *semantic* oracle substitutes for
  scale the way a *syntactic/compile* one does, or needs capability the weak model lacks.
