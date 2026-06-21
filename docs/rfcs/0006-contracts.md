# RFC-0006 — Optional runtime contracts

| Field | Value |
|---|---|
| Number | 0006 |
| Status | accepted |
| Created | 2026-06-19 |
| Supersedes | — |

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
exist to (1) turn the acceptance corpus into an **executable specification** —
`examples + contracts = executable spec` — deepening `run_ok` from
`exit + stdout` to `exit + stdout + stated invariants` and letting the corpus
(RFC-0004) grow from an acceptance suite into a behavioural specification of
the language and its libraries; (2) give code-generating agents that same
executable spec to attach and check, surfacing likely defects far earlier
than a type error would; and (3) differentiate Aril as *agent-productive yet
human-non-binding*.

The first of these is the quiet but strongest payoff: it is not really about
"contracts" as a feature, it is about what the corpus *becomes* once examples
can state and check their own intent.

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
non-negative). These are an orthogonal axis to the algebraic-type axis.
Contracts are how a program states them, and runtime checking is how the
corpus *observes* them.

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
(bespoke parser, weak IDE story) buys us nothing.

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
  ensures  match result { Ok(q) => q == a / b, Err(_) => true }
}
```

A `contract` block on a **type** carries its invariant:

```aril
type Interval = { lo: int, hi: int }

contract Interval {
  invariant lo <= hi
}
```

A type invariant is checked at two precisely-defined points: (1) immediately
after a value of the type is **constructed** (record literal / constructor),
and (2) at the **exit of every method declared on the type**. A multi-step
mutation performed *inside* a method may transiently break the invariant; it
is checked once, at the method's exit — the standard Design-by-Contract
window. To keep these the *only* checkpoints, **direct external field
assignment to an invariant-bearing type is rejected** (E1106): mutation goes
through a method, where the window is well-defined. So the sequence

```aril
var x = Interval{ lo: 1, hi: 9 }
x.lo = 10        // E1106 — assign via a method on Interval
x.hi = 5
```

does not compile; the same logic written as `x.widen(10, 5)` is checked once
at `widen`'s exit and the `lo <= hi` violation is caught there. (Whether to
instead admit external field writes as per-write checkpoints is recorded as
an open question.)

This rule covers **record `type` aliases**, not just classes: `Interval`
above is a record, and a record with no methods is checked at its **literal
construction** only — so, combined with the E1106 write-rejection, such a
value is validated once and then effectively frozen at its constructed shape.
A class with methods adds the method-exit checkpoints. The clean positive case
for this model is a class whose every mutation already goes through methods
(e.g. an LRU cache whose `size <= capacity` invariant is transiently broken
mid-`put` then restored before the method exits): it never trips E1106.

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
    // isSorted is an ordinary user predicate — a pure ([]int) -> bool func.
    // The invariant is over `xs` (sorted in place): result is ensures-only,
    // and mid-loop the function has not returned yet.
    invariant isSorted(xs[xs.len() - pass : xs.len()])
  }
}
```

A loop `invariant` is checked at loop entry and after each iteration. v1
deliberately stops at the invariant: a loop `variant` (a termination measure)
answers a different question — *does the algorithm terminate?* — and is the
start of a separate formal-verification branch, out of scope here.

**Inline clauses** are an optional convenience — the same `requires` /
`ensures` / `invariant` keywords written directly on a signature or loop
header, for authors who prefer the contract beside the code. They desugar to
exactly the same Contract IR as the block; the block is the primary form and
inline is sugar over it:

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
- **`old(e)`** — the value of pure expression `e` **as evaluated on entry**;
  legal only inside `ensures`. Lowering snapshots the *value* of `e` at
  function entry, not a reference to be dereferenced later. This matters when
  the body mutates a reference: to relate a reversed list's length to its
  original, write `old(listLen(head))` (snapshot the length on entry), **not**
  `listLen(old(head))` (which would walk the already-mutated nodes). The depth
  of the snapshot follows the value of `e` (the Performance note flags the cost
  of snapshotting a large aggregate).
- **`match` is a legal predicate expression.** Because `Option`/`Result` (and
  user sum types) have no methods, inspecting a sum payload is done by `match`
  returning a `bool` — e.g. `match result { Ok(v) => v >= 0, Err(_) => true }`.
  This is decisive: without it, every contract on Aril's `Result`/`Option`-
  centric surface would need a wrapper helper. (A discriminator sugar —
  `result is Ok` — is a possible future convenience over `match`.)
- For an `invariant`, the type's own fields are in scope (as in the
  `Interval` example).

Predicates must be **pure** (no I/O, no mutation, no `spawn`, no `try` that
escapes, no channel `send`/`recv`) — a contract that changes program
behaviour when enabled is a contradiction. Purity is checked, not trusted
(E1103). `implies` is sugar for `!a || b` admitted inside predicates for
readability.

Predicates are ordinary Aril, so they reuse the existing typechecker
end-to-end: an `ensures` that calls a user predicate function
(`isSorted(result)`) is just a typed call. The most common such helper is a
**bounded for-all over a collection** (`isSorted`, `allInRange`, `allDistinct`,
`isUnique`). v1 ships these as a small **standard predicate library**
(`std.pred`) — pure functions usable in any contract — so the common for-all
shapes need no re-writing, while the v1 language surface stays minimal (no new
quantifier expression form): an open-ended predicate sub-grammar risks bloating
toward a Raku-scale surface. A real quantifier (`forall x in coll: P`) is
deferred to v1.1; when it lands, the `std.pred` helpers are re-expressible on
top of it without breaking their call sites.

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
contracts and a blame calculus (Findler-Felleisen; Racket's Indy semantics)
are out of scope.

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
5. **Guard invariant types**: a direct external field assignment to a type
   that declares an `invariant` is E1106 (mutation must go through a method,
   the only invariant checkpoint besides construction).
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
func CheckLoop(mode Mode, ok bool, v Violation)   // loop invariant
func Stats() ViolationTally                        // for --contracts=stats
```

Codegen lowers each obligation to a guarded call at the boundary:
`requires` at function entry (after `old(e)` snapshots), `ensures` at each
return, a type `invariant` after construction and at each method's exit, and
a **loop** `invariant` at the labelled loop's entry and end of each
iteration. Under `off`, codegen emits nothing (no IR → no call), so the
elision is total and free.

### Corpus integration (RFC-0004 / D25) — the corpus becomes a spec

This is the centre of gravity of the RFC, not a side effect. RFC-0004
defines the corpus as an *acceptance suite* — a set of real programs that
prove the language can express and run them. Contracts turn each example
into an **executable specification of its own behaviour**: not just "this
program runs and prints X" but "this program runs, prints X, *and* its
stated invariants hold while it does." Summed over the corpus, that is a
growing, machine-checked behavioural specification of the language and the
libraries the examples exercise — the corpus stops being only an acceptance
gate and becomes a living spec.

Mechanically: `example.toml` gains an optional contract dimension. A
`run-pass` example may declare contracts in its source, and the run pass
executes it under `--contracts=panic`; a contract violation is then a **run
failure** — the example fails `run_ok` until its behaviour actually
satisfies its stated intent. This deepens `run_ok` from `exit + stdout` to
`exit + stdout + stated invariants` without a new metric in v1. (A dedicated
`contract_ok` tally — "examples whose contracts hold" — is a candidate
follow-up, mirroring how `diag_ok` grew beside `build_ok`.) Atomic coverage
(the hard rule) lands as fixtures under `tests/{grammar,sema,codegen}/` for
each new construct and E-code; live coverage is a few corpus examples
gaining `requires`/`ensures`.

## Non-goals — what contracts cannot express

Contracts are **point-in-time state assertions** evaluated at function entry,
each return, method exit, and loop entry/iteration. They read pure boolean
expressions over reachable state. By construction they **cannot express
liveness, termination, ordering, or protocol/session properties** — a
predicate only runs if control *reaches* its boundary.

Two concrete consequences:

- **Concurrency / channels.** Aril shares Go's channels, but a pre/post/
  invariant does **not** detect the channel bugs that matter: deadlock,
  goroutine leak, forgotten-close, send-on-closed / double-close / nil-channel
  panics, cross-goroutine ordering, or data races. A blocked goroutine never
  reaches its postcondition; a deadlock means the join never completes, so no
  downstream `ensures` ever runs — *the deadlock eats its own detector*. The
  corpus's one real channel bug (`rate_limited`'s deadlock) would be caught by
  **no** contract in this design. What contracts *do* buy for concurrency is
  narrow and worth stating honestly: **value-accounting on functions that
  return an aggregate** (`worker_pool`'s `result.len() == jobs.len()`,
  `concurrency`'s `count <= desired`) — the arithmetic of what flowed, not
  channel safety. Values emitted via `send` / consumed via `recv` are not even
  observable in predicates (`recv` is impure), so idiomatic pipeline code
  (`pipeline`, `select_showcase`) yields essentially no value contracts.
- **State-machine protocol.** A type `invariant` captures per-state data
  sanity and a single `step`'s legality, but not "no path from `Idle` to
  `Dispensing` without paying" — that quantifies over the *trace* of events,
  which pre/post/invariant cannot see.

These belong to other mechanisms: the Go runtime panic (send-on-closed),
the race detector (`-race`), structured-concurrency scope-join, static
analysis — and, for protocol/temporal properties, a separate **trace / session
contract** branch that is a *different mechanism*, not an extension of
pre/post/invariant (RFC-0007).

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

## Transition / compatibility

Strictly additive. No existing program changes meaning; with no contracts
the new pass is a no-op and codegen output is byte-identical (golden-fixture
and `build_ok` ratchet safe). Default mode for `run`/corpus is `panic`;
existing examples have no contracts and are unaffected until contracts are
added to them deliberately. No deprecation window needed.

## Performance note

Two costs are worth flagging up front so they are not re-litigated later:

- **`old(e)` snapshots the evaluated value at function entry.** For a large
  argument (`old(bigTree)`) that snapshot can be expensive in time and
  memory. v1's stance: it is the author's call, but the `internal/contract`
  pass **may warn** when an `old(e)` snapshots a large/aggregate value, and a
  future static pass may elide snapshots provably unread on a failing path.
- **Per-iteration loop invariants multiply cost.** A loop invariant runs
  every iteration; under `panic`/`warn` this is real overhead. The `off`
  mode compiles all of it out (no IR → no call), and `stats` keeps the
  counting cheap — so perf-sensitive builds have an escape that the corpus's
  `panic` build does not need.

## History

- 2026-06-20 — `draft → accepted`.
