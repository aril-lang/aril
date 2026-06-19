# RFC-0006 — Optional runtime contracts

| Field | Value |
|---|---|
| Number | 0006 |
| Status | draft |
| Created | 2026-06-19 |
| Supersedes | — |
| Target | `lang-spec/grammar.ebnf` (the `contract` block production + inline clauses), `lang-spec/type-system.md` (T-Contract-Pred, T-Old, T-Result), `lang-spec/diagnostics.md` (E11xx contract codes), `lang-spec/lowering-go.md` (§ContractIR lowering + mode dispatch), `lang-spec/builtins.md` (`old`/`result` predeclared in contract scope); `examples/README.md` + RFC-0004 corpus metadata (`example.toml` contract block); `docs/rfcs/README.md` index row |

## Summary

Add **optional, runtime-checked contracts** to Aril — preconditions
(`requires`), postconditions (`ensures`, with `old(e)` and `result`), and
type invariants (`invariant`) — written primarily in a **separable
`contract` block** that attaches obligations to an existing declaration by
name, with optional inline clauses as sugar. The block is hierarchical: it
carries a function's pre/post and, nested by loop label, **per-loop
invariants** — so a function's whole behavioural spec (including its loops)
lives beside the code without touching the body. Contracts are pure boolean
Aril expressions.
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

### Surface — the `contract` block is primary

The canonical form is a **separable `contract` block** that attaches
obligations to an existing declaration by name. It leaves the declaration's
signature and body untouched — which is exactly what makes contracts
genuinely non-binding (a human writes ordinary Aril, with no contract
syntax in sight) and what enables the agent-symbiosis (an agent bolts
contracts onto code it did not write, by name, without editing it):

```aril
func safeDivide(a: int, b: int): Result<int, DivError> {
  return Ok(a / b)
}

contract safeDivide {
  requires b != 0
  ensures  result.isOk() implies result.unwrap() * b <= a
}
```

A `contract` block on a **type** carries its invariant:

```aril
type Interval = { lo: int, hi: int }

contract Interval {
  invariant lo <= hi
}
```

**Loop contracts.** A loop is anonymous, so it has no name for the block to
target. A loop that bears a contract is **labelled**, and the function's
`contract` block carries a nested `loop <label>` section — keeping the whole
function spec in one separable block, body untouched:

```aril
func bubbleSort(xs: []int): []int {
  for pass in 0 .. xs.len() loop outer {
    for i in 0 .. xs.len() - pass - 1 { ... }
  }
  return xs
}

contract bubbleSort {
  ensures isSorted(result)
  loop outer {
    invariant isSorted(slice(result, result.len() - pass, result.len()))
    variant   xs.len() - pass            // optional decreasing measure
  }
}
```

A loop `invariant` is checked at loop entry and after each iteration; an
optional `variant` is a pure int measure asserted to strictly decrease and
stay `>= 0` each iteration (a runtime guard against a class of
non-termination/logic bugs — full termination *proof* is static and
deferred, §Open questions).

**Inline clauses** are an optional convenience — the same `requires` /
`ensures` / `invariant` / `variant` keywords written directly on a
signature or loop header, for authors who prefer the contract beside the
code. They desugar to exactly the same Contract IR as the block; the block
is the primary form and inline is sugar over it:

```aril
func abs(x: int): int
  ensures result >= 0
{ ... }
```

Both surfaces desugar to the same Contract IR attached to the symbol (or,
for a loop, to the labelled loop within it). A declaration may carry inline
clauses *and* a block; they union (a duplicate identical clause is a
warning, not an error).

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

1. **Bind** inline clauses and `contract` blocks (incl. nested `loop
   <label>` sections) to their target symbols; a block naming an unknown
   decl, or a `loop` section naming an unknown/unlabelled loop, is E1101.
2. **Type-check** each predicate as `bool` in the right scope (reusing
   `sema.Info`); non-bool is E1102.
3. **Purity-check** predicates; an impure predicate is E1103.
4. **Scope-check** `old`/`result`: `result` or `old` outside `ensures` is
   E1104; `old(e)` over an impure/forbidden `e` is E1105.
5. **Well-form** `variant`: a `variant` measure that is not a pure int
   expression is E1106.
6. **Lower** to `contract.IR` — a per-symbol list of obligations
   (`{kind, predExpr, oldSnapshots, loopLabel?, srcSpan}`) consumed by
   codegen.

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
func CheckPre(mode Mode, ok bool, v Violation)    // requires
func CheckPost(mode Mode, ok bool, v Violation)   // ensures / invariant
func CheckLoop(mode Mode, ok bool, v Violation)   // loop invariant / variant
func Stats() ViolationTally                        // for --contracts=stats
```

Codegen lowers each obligation to a guarded call at the boundary:
`requires` at function entry (after `old(e)` snapshots), `ensures` at each
return, `invariant` around exported method boundaries on the type, and a
**loop** `invariant`/`variant` at the labelled loop's entry and end of each
iteration (the `variant` lowering also stashes the prior measure to assert
strict decrease). Under `off`, codegen emits nothing (no IR → no call), so
the elision is total and free.

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

- `lang-spec/grammar.ebnf` — the `contract <name> { … }` block production
  (primary), incl. nested `loop <label>` sections; a loop `label` on a loop
  header; inline `requires`/`ensures`/`invariant`/`variant` clauses as sugar.
- `lang-spec/type-system.md` — T-Contract-Pred (predicate : bool, pure),
  T-Result (`result` typed as the return type, `ensures`-only), T-Old
  (`old(e)` typed as `e`, `ensures`-only).
- `lang-spec/builtins.md` — `old`/`result` as contract-scope predeclared
  identifiers; `implies` predicate sugar.
- `lang-spec/diagnostics.md` — E1101 (contract on unknown decl / loop
  label), E1102 (non-bool predicate), E1103 (impure predicate), E1104
  (`old`/`result` outside `ensures`), E1105 (`old` over forbidden expr),
  E1106 (`variant` not a pure int measure).
- `lang-spec/lowering-go.md` — §ContractIR: snapshot/entry/exit lowering,
  per-iteration loop-invariant/`variant` lowering, and the four-mode
  dispatch into `arilrt`.
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
2. **Loop `variant` / termination depth.** v1 checks loop **invariants** at
   runtime and offers a `variant` as a runtime strict-decrease guard, but
   does not *prove* termination — a true termination proof is static
   (Eiffel/Ada discharge it; we defer it to the static-discharge path, #3).
   Open: is the runtime `variant` guard worth its cost, or invariant-only?
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
