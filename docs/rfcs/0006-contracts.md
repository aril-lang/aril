# RFC-0006 — Optional runtime contracts

| Field | Value |
|---|---|
| Number | 0006 |
| Status | draft |
| Created | 2026-06-19 |
| Supersedes | — |
| Target | `lang-spec/grammar.md` (contract clauses + sidecar block), `lang-spec/type-system.md` (T-Contract-Pred, T-Old, T-Result), `lang-spec/diagnostics.md` (E07xx contract codes), `lang-spec/lowering-go.md` (§ContractIR lowering + mode dispatch), `lang-spec/builtins.md` (`old`/`result` predeclared in contract scope); `examples/README.md` + RFC-0004 corpus metadata (`example.toml` contract block); `docs/rfcs/README.md` index row |

## Summary

Add **optional, runtime-checked contracts** to Aril — preconditions
(`requires`), postconditions (`ensures`, with `old(e)` and `result`), and
type invariants (`invariant`) — written either inline on a declaration or
in a **separable `contract` block** that attaches obligations to an
existing declaration by name. Contracts are pure boolean Aril expressions.
They are enforced by the runtime under one of four selectable modes —
**panic / warn / stats / off** — and lower to guarded checks in `arilrt`.
Two new compiler surfaces own them: a compile-time **`internal/contract`**
pass (a sema sibling that type-checks and purity-checks predicates and
lowers them to a Contract IR) and an **`arilrt/contract.go`** runtime layer
(mode dispatch, blame, violation rendering).

Contracts are **never required**. They change no existing program. They
exist to (1) enrich the corpus's *behavioural* signal — the `run_ok`
metric today checks only exit code and stdout, so a program that "runs"
can still be silently wrong; (2) give code-generating agents an
**executable specification** they can attach and check, surfacing likely
defects far earlier than a type error would; and (3) differentiate Aril as
*agent-productive yet human-non-binding*.

## Motivation

### The corpus proves too little

`run_ok` (RFC-0004, D25) runs each `run-pass` example and checks exit code
+ stdout against a sidecar. That catches crashes and gross output drift,
but it cannot see that `merge_intervals` returned an *unsorted* list, that
`safeDivide` produced a result violating `result >= 0`, or that a
state-machine transition left an inconsistent record — as long as the
final printed line matches. Half of "does it work" is invisible to the
metric. The acceptance corpus, our primary feedback loop for "what works
and what does not yet," is behaviourally shallow precisely where it should
be deep.

Aril's type system already makes illegal *shapes* unrepresentable — sum
types, exhaustive match, `Option`/`Result`. What it structurally cannot
express are *value relationships*: ranges (`0 <= p <= 100`), cross-field
invariants (`lo <= hi`), size relations (output length = input length),
ordering (sortedness), and input↔output postconditions (`abs` returns
non-negative). These are an orthogonal axis to the algebraic-type axis (see
*Prior art*, §refinement-vs-ADT). Contracts are how a program states them,
and runtime checking is how the corpus *observes* them.

### Code agents are the differentiating audience

Aril targets TypeScript developers (D2), who have never been asked to live
inside Eiffel/SPARK-style rigor — and making such rigor *mandatory* would
be hostile to that community. So contracts must stay optional. But the
audience that benefits most is **code-generating agents**:

- An agent that writes a function *and its contract* gets a tight,
  executable oracle. A violated `ensures` is a precise, localized,
  blame-carrying failure the agent can read and fix — surfaced at the first
  test run, not after a human notices wrong output three steps downstream.
  This shortens the agent's generate→observe→repair loop, which is the
  dominant cost in agentic coding.
- Contracts double as **property-test oracles**: a precondition is a
  generator constraint, a postcondition is the oracle. An agent can drive
  generative testing from contracts it already wrote.
- **Symbiosis.** A human writes the code; an agent *attaches* contracts to
  it — the separable `contract <name> { … }` block does exactly this
  without touching the body. The human keeps writing ordinary Aril; the
  agent layers checkable intent on top. Neither is forced into the other's
  discipline.

This is the positioning claim worth differentiating on: **Aril is
non-binding for humans and contract-productive for agents, and supports a
human-agent symbiosis where intent is added incrementally.** No mainstream
language with a strong ADT core currently offers optional contracts framed
for the agent loop.

### We own the toolchain, so the JML tax is avoidable

JML embeds contracts in Java *comments* because a stock `javac` must ignore
them. Aril owns its lexer, parser, sema, and codegen — we have no such
constraint. Contracts can be real grammar, type-checked by real sema, with
real diagnostics in Aril coordinates (D10). The comment-DSL tradeoff
(bespoke parser, weak IDE story) buys us nothing and is rejected (§Alternatives).

## Design

### Surface — two forms, one semantics

**Inline clauses** on a function, between the signature and the body:

```aril
func safeDivide(a: int, b: int): Result<int, DivError>
  requires b != 0
  ensures  old(a) == a            // pure: inputs unchanged
{
  return Ok(a / b)
}
```

**Separable `contract` block**, attaching the same obligations to an
existing declaration by name (the agent-symbiosis and corpus-enrichment
vehicle — bolt contracts onto code without editing it):

```aril
contract safeDivide {
  requires b != 0
  ensures  result.isOk() implies result.unwrap() * b <= a
}
```

**Type invariant**, inline in a type decl or via a `contract` block on the
type:

```aril
type Interval = { lo: int, hi: int }
  invariant lo <= hi
```

Both surfaces desugar to the same Contract IR attached to the symbol. A
declaration may carry inline clauses *and* a sidecar block; they union (a
duplicate identical clause is a warning, not an error).

### Predicate language

A contract predicate is a **pure boolean Aril expression** evaluated in the
declaration's scope, extended with:

- **`result`** — the return value; legal only inside `ensures`.
- **`old(e)`** — the value of pure expression `e` on entry; legal only
  inside `ensures`. Lowering snapshots `old(e)` at function entry.
- For an `invariant`, the type's own fields are in scope (as in the
  `Interval` example).

Predicates must be **pure** (no I/O, no mutation, no `spawn`, no `try` that
escapes) — a contract that changes program behaviour when enabled is a
contradiction. Purity is checked, not trusted (§E07xx). `implies` is sugar
for `!a || b` admitted inside predicates for readability.

Predicates are ordinary Aril, so they reuse the existing typechecker
end-to-end: an `ensures` that calls a user predicate function
(`isSorted(result)`) is just a typed call.

### Enforcement modes

One global mode per build, selected by `--contracts=<mode>` (and a
per-profile default):

| Mode | On violation | Use |
|---|---|---|
| **panic** | abort with a contract-violation diagnostic | corpus run pass; CI; agent oracle |
| **warn** | print the violation, **continue** | dev introspection without halting |
| **stats** | count silently, continue; dump a tally at exit | profiling which contracts fire, perf-sensitive runs |
| **off** | checks compiled out entirely | release / zero-overhead |

This is the C++26 `enforce/observe/ignore` taxonomy plus a `stats`
variant; the mode is a *build* choice, so the same source runs checked or
unchecked. Default: `panic` for `aril run`/the corpus, `off`-equivalent
elision left to a later release profile.

### Blame and violation reporting

Attribution follows the standard structural convention: a **`requires`
failure blames the caller**, an **`ensures`/`invariant` failure blames the
callee**. A violation carries: the kind (pre/post/invariant), the Aril
source coordinates of the clause (D10), the predicate source text, and the
bound values of its free variables (icontract-style rendering — "`b != 0`
with `b = 0`"). First-order only in v1; higher-order (function-valued)
contracts and a blame calculus are explicitly out of scope (§Open questions).

### Module (a) — compile-time: `internal/contract`

A sema sibling, not a sema extension, so the boundary stays clean and an
eventual self-host slice is isolable. Interface:

```
contract.Check(prog *ast.Program, types *sema.Info) (*contract.IR, []diag.Diagnostic)
```

Responsibilities:

1. **Bind** inline clauses and `contract` blocks to their target symbols;
   a block naming an unknown decl is E0701.
2. **Type-check** each predicate as `bool` in the right scope (reusing
   `sema.Info`); non-bool is E0702.
3. **Purity-check** predicates; an impure predicate is E0703.
4. **Scope-check** `old`/`result`: `result` or `old` outside `ensures` is
   E0704; `old(e)` over an impure/forbidden `e` is E0705.
5. **Lower** to `contract.IR` — a per-symbol list of obligations
   (`{kind, predExpr, oldSnapshots, srcSpan}`) consumed by codegen.

The pass is **purely additive**: a program with no contracts produces an
empty IR and is byte-identical through codegen (golden-fixture safe). It is
the natural future home for *static discharge* — obligations a future
refinement-style checker can prove are dropped from the runtime IR; the
gradual-verification door (prove what you can, check the rest) is left open
by construction but unimplemented in v1.

### Module (b) — runtime: `arilrt/contract.go`

The checked-evaluation layer, mode-aware, part of the runtime contract
(D18, dual-mode per D26 — inline and vendored emit the same surface):

```
type Violation struct { Kind, Pred, Where string; Bindings []Binding }
func CheckPre(mode Mode, ok bool, v Violation)   // requires
func CheckPost(mode Mode, ok bool, v Violation)  // ensures / invariant
func Stats() ViolationTally                       // for --contracts=stats
```

Codegen lowers each obligation to a guarded call at the boundary:
`requires` at function entry (after `old(e)` snapshots), `ensures` at each
return, `invariant` around exported method boundaries on the type. Under
`off`, codegen emits nothing (no IR → no call), so the elision is total and
free.

### Corpus integration (RFC-0004 / D25)

`example.toml` gains an optional contract dimension: a `run-pass` example
may declare contracts in its source, and the run pass executes it under
`--contracts=panic`. A contract violation is then a **run failure** — the
example fails `run_ok` until its behaviour actually satisfies its stated
intent. This deepens `run_ok` from "exit+stdout" to "exit+stdout+stated
invariants" without a new metric in v1. (A dedicated `contract_ok` tally —
"examples whose contracts hold" — is a candidate follow-up, mirroring how
`diag_ok` grew beside `build_ok`.) Atomic coverage (the hard rule) lands as
fixtures under `tests/{grammar,sema,codegen}/` for each new construct and
E-code; live coverage is a few corpus examples gaining `requires`/`ensures`.

## Alternatives considered

- **Comment-embedded DSL (JML-style).** Rejected: JML's rationale is that a
  stock compiler must ignore the spec. We own the compiler, so this only
  costs us a bespoke parser, weaker diagnostics, and no IDE story. (Prior
  art: OpenJML.)
- **Library DSL of combinators (Racket/Clojure-spec/zod-style).** Viable
  with no grammar change, and how dynamically-typed languages get contracts.
  Rejected as the *primary* form because postconditions and `old` are
  awkward as library calls, and because first-class clauses give better
  diagnostics and a cleaner lowering. The separable `contract` block
  recovers the main ergonomic win (attach-without-editing) of the library
  form within first-class syntax.
- **Refinement types / static-only contracts (Liquid Haskell / SPARK).**
  The strongest guarantee — proved for all executions — but requires
  restricting predicates to an SMT-decidable fragment, or accepting
  undecidability with manual proof. Too heavy for a TS-developer audience
  and premature for the toolchain. Runtime contracts need no decidability
  ceiling (any predicate runs) and yield true counterexamples with blame.
  The `internal/contract` IR keeps the static-discharge door open for a
  future gradual path (prove the decidable subset, check the rest).
- **Compile-time-only hints (Kotlin `contract {}`).** Rejected: Kotlin's
  contracts are *unchecked promises* that only steer smart-casts and are
  silently unsound if violated. That gives the agent loop nothing to
  observe. We want *checked* obligations.
- **Do nothing; grow `run_ok` with more stdout assertions.** Rejected:
  stdout assertions are external, brittle, and cannot state intra-program
  invariants (sortedness, cross-field relations) without contorting the
  program to print them.

## Prior art

Surveyed for this RFC (citations for the paper trail):

- **Design by Contract** — Meyer, *Eiffel* (SciComp. Prog. 1988). Pre/post/
  invariant as runtime-checked Hoare triples; static proof is an optional
  add-on, not the base guarantee.
- **In-language forms** — Eiffel (`require`/`ensure`/`invariant`), Ada 2012
  aspects (`Pre`/`Post`/`Type_Invariant`, `Assertion_Policy`
  Check/Ignore/Disable), D (`in`/`out`/`invariant`, `-release` off), C++26
  P2900 (`pre`/`post`/`contract_assert`; ignore/observe/enforce/quick-enforce,
  build-selected). The mode taxonomy here is drawn from these.
- **Comment-DSL** — JML (`//@ requires`), justified by Java toolchain
  opacity; inapplicable to Aril (we own the compiler).
- **Library DSL** — Racket `contract-out` (the formal **blame** system,
  Findler & Felleisen ICFP 2002; correct blame, Dimoulas et al. POPL 2011),
  Clojure spec/Malli (`s/fdef` args/ret/fn + generative checking), Python
  icontract/deal (decorators, rendered-value violations), TS zod/io-ts/typia
  (runtime structural validation, since TS erases types).
- **Refinement-vs-ADT** — an ADT + exhaustive-match + `Option`/`Result`
  system constrains *shape*; it cannot express value ranges, cross-field/
  size/order relations, or input↔output postconditions (Liquid Haskell,
  Vazou et al. ICFP 2014; refinements as statically-discharged contracts,
  SMT-decidable fragment). Contracts add the orthogonal value-relation axis.
- **Static↔dynamic spectrum** — gradual typing (Siek & Taha 2006), hybrid
  type checking (Flanagan POPL 2006: check at runtime what isn't provable
  statically), gradual verification (Bader/Aldrich/Tanter VMCAI 2018).
  Frames the future static-discharge door.

## Paired edits

On acceptance, the implementing PRs touch:

- `lang-spec/grammar.md` — `requires`/`ensures`/`invariant` clauses on
  decls; the `contract <name> { … }` block production.
- `lang-spec/type-system.md` — T-Contract-Pred (predicate : bool, pure),
  T-Result (`result` typed as the return type, `ensures`-only), T-Old
  (`old(e)` typed as `e`, `ensures`-only).
- `lang-spec/builtins.md` — `old`/`result` as contract-scope predeclared
  identifiers; `implies` predicate sugar.
- `lang-spec/diagnostics.md` — E0701 (contract on unknown decl), E0702
  (non-bool predicate), E0703 (impure predicate), E0704 (`old`/`result`
  outside `ensures`), E0705 (`old` over forbidden expr).
- `lang-spec/lowering-go.md` — §ContractIR: snapshot/entry/exit lowering and
  the four-mode dispatch into `arilrt`.
- RFC-0004 corpus metadata (`example.toml`) — the run-pass contract
  dimension; `examples/README.md` note.
- `docs/rfcs/README.md` — index row.

Atomic fixtures (hard rule) accompany each new construct and E-code in
`tests/{grammar,sema,codegen}/`.

## Transition / compatibility

Strictly additive. No existing program changes meaning; with no contracts
the new pass is a no-op and codegen output is byte-identical (golden-fixture
and `build_ok` ratchet safe). Default mode for `run`/corpus is `panic`;
existing examples have no contracts and are unaffected until contracts are
added to them deliberately. No deprecation window needed.

## Open questions

1. **Higher-order contracts.** Function-valued contracts need wrapping/
   proxying and contravariant blame (Findler-Felleisen; Racket's Indy
   semantics). v1 is first-order only. Worth it given Aril's uncolored
   closures? *Deferred.*
2. **Loop invariants / `variant` (termination).** Eiffel/Ada have them;
   they matter mainly for static proof. *Out of scope for v1.*
3. **Static discharge.** When does the `internal/contract` IR gain a
   prove-and-drop path (gradual verification)? Needs the decidable-fragment
   decision. *Deferred — the IR is shaped to allow it.*
4. **`contract_ok` as a fourth corpus metric** vs. folding contract checks
   into `run_ok`. v1 folds in; revisit if the signal deserves its own floor.
5. **Mode granularity.** Global `--contracts=` only, or per-module/per-decl
   override? Start global. *Deferred.*
6. **Exposing contracts to the agent via the reflection/REPL surface**
   (RFC-0003) so an agent can enumerate a function's obligations
   programmatically. *Deferred — promising for the agent-loop story.*
