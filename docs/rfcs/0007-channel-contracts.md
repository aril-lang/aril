# RFC-0007 — Channel contracts (trace contracts over channel events)

| Field | Value |
|---|---|
| Number | 0007 |
| Status | draft |
| Created | 2026-06-20 |
| Supersedes | — |

## Summary

Add optional, runtime-checked **channel contracts**: predicates over the
**observable trace of channel events** — `send`, `recv`, `close`, `cancel`,
`timeout`. A channel contract does not describe how a goroutine is
implemented; it describes the *observable sequence* of events on a channel,
in three Go-readable kinds:

- **safety** — forbidden event patterns ("bad things never happen"): never
  send after close; never `send(Result{id})` before `recv(Job{id})`; never
  more than N in flight;
- **liveness** — events that must eventually happen ("good things eventually
  happen"): every `recv(Job{id})` is eventually followed by `send(Result{id})`
  or `send(Error{id})`; `out` eventually closes;
- **fairness** — no declared participant is starved forever: a `select` does
  not ignore one input indefinitely.

This borrows the useful mental model of **TLA+ (safety / liveness / fairness)
without exposing any temporal-logic syntax** — TLA+ semantics underneath, a
Go-readable protocol language above. Contracts are declared in RFC-0006's
separable `contract` block, share its four modes (panic / warn / stats / off)
and `arilrt` runtime layer, and report in Aril source coordinates (D10).

This is the trace-contract branch RFC-0006 deferred in its §Non-goals — a
*different mechanism* from point-in-time pre/post/invariant. It targets the
channel-protocol bugs that Aril's structured concurrency, the Go runtime, and
`-race` do **not** already cover.

## Motivation

### The residual that nothing else covers

Empirically, message passing is *more* blocking-bug-prone than shared memory
in real Go systems — of 85 blocking bugs studied across Docker / Kubernetes /
etcd / CockroachDB / gRPC-go / BoltDB, channel misuse (missing send/recv,
premature/erroneous close) is the single largest cause (Tu et al., ASPLOS
2019). The fixes are small and local — i.e. *locally specifiable*, exactly
what a contract can state.

Aril already disposes of much of the space *by construction*, so a channel
contract must target only what remains:

- **Goroutine leaks / orphaned tasks / un-cancelled context** — eliminated by
  structured concurrency: a `spawn` cannot outlive its `scope`. No contract
  needed.
- **Global deadlock** (all goroutines asleep) — the Go runtime already panics.
- **Data races** — `go test -race` already covers these.

What is left — and what this RFC targets — is the **channel event protocol**:
close-safety (send-on-closed, double-close, send-after-close), completion
(producer closed early / consumer expected more), ordering, and the
bounded-liveness / fairness signals around partial deadlock and starvation
that Go's *global*-only detector misses (it caught 2 of 21 blocking bugs in
the study).

### The guard-rail: a TS/Go developer must read any contract in five minutes

Session types historically grow into monsters: `send`/`recv` becomes choice
and branch becomes loop becomes a recursive protocol algebra, and a few years
later nobody uses it. The governing design constraint of this RFC is therefore
a **readability test**, applied to every construct:

> Can any channel contract be explained to an ordinary TS/Go developer in five
> minutes? If understanding a contract requires reading a 40-page paper on the
> π-calculus, we have lost.

So we keep the *mental model* of TLA+ — the safety / liveness / fairness
split — and deliberately leave its *machinery* out (see §The guard-rail).

## Design

### Channel contracts are trace contracts

A channel contract is evaluated over a **trace** of observable channel events:

```
send · recv · close · cancel · timeout
```

It describes the observable protocol, not the implementation. Two consequences
make this both precise and teachable, stated in plain terms:

- **Only declared channel events are observed.** Unrelated computation steps
  are invisible to the contract — it does not matter how many internal loops a
  worker runs between `recv(job)` and `send(result)`. (This is TLA+'s
  stuttering-insensitivity, without the name.)
- **The contract is an external protocol, not a script.** A real program may
  perform many more internal actions; only its *observable channel events*
  must conform to the contract. (This is refinement, in plain terms.)

Because the model is a trace of named events, the clauses read like English:

```
forbid send(Result{id}) before recv(Job{id})
every recv(Job{id}) eventually send(Result{id}) or send(Error{id})
eventually close(out) after close(in)
```

### Three kinds: safety, liveness, fairness

Every channel clause is one of three kinds — the TLA+ division, in
Go-readable form. Each kind has a different, honestly-stated runtime status.

**1. Safety — "bad things never happen" (forbidden event patterns).**
The most understandable and the only *definitively* runtime-checkable kind: a
forbidden event, when it occurs, is caught at that instant with blame.

```
contract runPool {
  channel work {
    closed-by feed                 // only `feed` may close `work`
    forbid send after close        // send-after-close is forbidden
  }
  channel results {
    closed-by pool
    forbid send(Result{id}) before recv(Job{id})   // ordering
    never more than jobs.len() in flight            // bound
    drains-before-scope-exit                        // completion, at scope exit
  }
}
```

A safety violation is definitive (a finite trace *proves* it) and fires in any
mode. Close-ownership and send-after-close are the high-frequency subset;
`forbid <A> before <B>`, `never more than N in flight`, and
`drains-before-scope-exit` (the channel is closed and empty when its owning
`scope` returns) generalise it to ordering, capacity, and completion.

**2. Liveness — "good things eventually happen" (must-eventually events).**
More important for concurrency than ordinary postconditions: a function can
violate no safety rule and still hang forever.

```
  channel results {
    every recv(Job{id}) eventually send(Result{id}) or send(Error{id})
    eventually close after close(work)
  }
```

A runtime monitor can never *refute* a pure "eventually" from a finite trace —
the awaited event might still come. So liveness in v1 is **runtime-checkable
only in a bounded / test mode** (a deadline turns "eventually" into "within
this bound for this run"), and a violation is reported honestly as a
**bounded, non-definitive** signal — never as a proof of deadlock.

**3. Fairness — "no participant is starved forever".**
The riskiest TLA+ idea, kept in its most human form — no weak/strong fairness,
just "do not ignore one input indefinitely":

```
contract Merge {
  fairness {
    no-starvation inputA
    no-starvation inputB
  }
}
```

Fairness is **observable/testable intent, not a v1 proof obligation**: a
stress run may search for starvation, but the contract does not claim to prove
its absence.

### The guard-rail — what we deliberately do *not* bring

The mental model is TLA+; the surface is Go. We take the safety / liveness /
fairness *distinction* and the *trace-of-events* model, and we leave out
everything that fails the five-minute test:

- temporal-logic operators (`[]`, `<>`, `~>`, `WF`, `SF`);
- state predicates with primed variables;
- a full action algebra;
- model-checking terminology in the surface language;
- a recursive protocol calculus / session-type algebra.

If a construct cannot be explained to a TS/Go developer in five minutes, it
does not enter the surface.

### Diagnostics

Grouped by kind. Safety codes are definitive; liveness/fairness codes are
bounded/testable signals, reported as non-definitive.

- **Safety:** E1201 (close by a non-owner — violates `closed-by`), E1202
  (double close), E1203 (send after close), E1204 (a declared
  `forbid <A> before <B>` / `never more than N` pattern violated), E1205
  (incomplete drain at owning-scope exit — producer closed early / consumer
  expected more).
- **Well-formedness:** E1206 (a channel, event, or site named in a clause is
  unknown or not in scope).
- **Liveness (bounded, non-definitive):** E1207 (a required `eventually` event
  not observed within the bound).
- **Fairness (testable, non-definitive):** E1208 (starvation of a declared
  participant observed under a stress run).

### Runtime — `arilrt` trace monitor

Under contract, a `Channel<T>` lowers to a thin wrapper that appends each
`send` / `recv` / `close` / `cancel` / `timeout` to a per-channel event trace
and evaluates the declared clauses against it:

```
func TraceEvent(mode Mode, c *CChan, ev Event)             // append + check safety
func TraceDrain(mode Mode, c *CChan, v Violation)          // completion at scope exit
func TraceLiveness(mode Mode, c *CChan, bound Duration, v Violation)  // bounded eventually
func TraceFairness(mode Mode, c *CChan, v Violation)       // starvation, stress mode
```

Blame is local and decentralized: per-channel checks compose into a global
guarantee (the multiparty-session-monitor result), so a violation names the
offending channel, the event, and the goroutine/role — in Aril coordinates
(D10). Modes panic / warn / stats / off and the elision-under-`off` story are
exactly RFC-0006's (no contract → no wrapper → byte-identical codegen). Count
and value relations on the *payloads* (`sends == recvs`, "exactly N items
flow") are not channel clauses — they are a value `ensures` (RFC-0006) on the
draining function.

## Alternatives considered

- **Declared session protocols / session-type algebra (Scribble / MPST).** A
  channel declares its full protocol (`recv Job; send Result; choice …; loop
  …`), projected to per-endpoint monitors. Rejected as the surface model: this
  is the path that grows into a recursive protocol algebra and fails the
  five-minute test. We keep the *trace* model (forbidden / eventual / fair
  events), which expresses the high-frequency cases in English; full ordered
  session protocols are a possible later, separate concern.
- **Infer-from-code static deadlock checking (Godel / Gong).** Infer a
  behavioural type from `scope`/`spawn`/channel code and model-check partial
  deadlock + liveness statically (Lange/Ng/Toninho/Yoshida). The strongest
  guarantee, zero surface — but a heavy implementation (an mCRL2-style
  checker), bounded analysis, and *static-first*, against the runtime-first
  stance. A future static path; the same trace clauses can later drive it.
- **Extend RFC-0006 pre/post to channels.** Impossible by construction: pre/
  post/invariant are point-in-time state assertions over one call; they cannot
  see a *sequence* of events (RFC-0006 §Non-goals). This RFC is a different
  mechanism — a predicate over a trace — deliberately separate.
- **Rely on the Go runtime + `-race`.** They cover the wrong things — global
  deadlock only, and data races only — leaving the channel event protocol (the
  largest blocking-bug source) uncovered.

## Prior art

- **TLA+** (Lamport) — the source of the **safety / liveness / fairness**
  division and the **trace-of-events / stuttering / refinement** model we
  borrow. We take the mental model only; none of its temporal-logic syntax,
  primed-variable state predicates, or action algebra enters the surface.
- **Trace contracts** — Moy & Felleisen, "Trace Contracts" (JFP 2023): a
  predicate over an accumulated event trace — the formal shape of a channel
  contract here.
- **Monitorability** — Leucker & Schallhart 2009; Havelund & Peled (RV 2023):
  safety is monitorable from a finite trace, pure liveness is not — the basis
  for "safety is definitive, liveness/fairness are bounded/testable."
- **Runtime session monitoring** — Bocchi/Chen/Demangeon/Honda/Yoshida (FORTE
  2013 / TCS 2017): local per-endpoint monitors compose to a global guarantee
  — the blame model adopted here.
- **Empirical grounding** — Tu, Liu, Song, Zhang, ASPLOS 2019: message passing
  causes more blocking bugs than shared memory; channel misuse dominates.
- **Channel typestate** — typestate (Strom & Yemini 1986; Aldrich et al.,
  Plaid 2009): `Open → Closed`, `send` legal only while `Open` — the safety
  subset (close-ownership / send-after-close), here generalised to a trace.

## Transition / compatibility

Strictly additive. No existing program changes meaning; a channel with no
contract is lowered exactly as today (the Go runtime's existing close-panic
semantics are unchanged). Default mode for `run`/corpus is `panic`; liveness
and fairness clauses are evaluated only in a bounded / stress mode and always
reported as non-definitive. No deprecation window.

## History

- 2026-06-20 — created (`draft`).
