# RFC-0007 — Channel contracts (trace contracts over channel events)

| Field | Value |
|---|---|
| Number | 0007 |
| Status | draft |
| Created | 2026-06-20 |
| Supersedes | — |

## Summary

Add optional, runtime-checked **channel contracts**: predicates over the
**observable trace of channel events**. Every event has the uniform form
`subject.operation(payload)` — a `send` / `recv` / `close` on a *named*
channel-like **subject**. A contract describes the observable protocol, not
the implementation, at two levels:

- **channel clauses** — local correctness of one channel (`closed-by`,
  `forbid send after close`, `never more than N in flight`);
- **protocol clauses** — cross-channel correctness in a single namespace
  (`forbid results.send(Result{id}) before work.recv(Job{id})`,
  `eventually results.close after work.close`).

Three kinds of clause, borrowing the **TLA+ mental model — safety / liveness /
fairness — without any temporal-logic syntax** (TLA+ semantics underneath, a
Go-readable protocol language above), plus a distinct **fan-out** obligation
(`delivered-to-all { … }`) for events that must reach *every* member of a
participant set. Contracts share RFC-0006's separable block, its four modes
(panic / warn / stats / off), and `arilrt` runtime layer, and report in Aril
source coordinates (D10).

This is the trace-contract branch RFC-0006 deferred — a *different mechanism*
from point-in-time pre/post/invariant. It targets the channel-protocol bugs
that Aril's structured concurrency, the Go runtime, and `-race` do not cover.

## Motivation

### The residual that nothing else covers

Empirically, message passing is *more* blocking-bug-prone than shared memory
in real Go systems — of 85 blocking bugs studied across Docker / Kubernetes /
etcd / CockroachDB / gRPC-go / BoltDB, channel misuse (missing send/recv,
premature/erroneous close) is the single largest cause (Tu et al., ASPLOS
2019). The fixes are small and local — exactly what a contract can state.

Aril already disposes of much of the space *by construction*, so a channel
contract targets only what remains:

- **Goroutine leaks / orphaned tasks / un-cancelled context** — eliminated by
  structured concurrency: a `spawn` cannot outlive its `scope`.
- **Global deadlock** (all goroutines asleep) — the Go runtime already panics.
- **Data races** — `go test -race` already covers these.

What is left is the **channel event protocol**: close-safety, ordering,
completion, fan-out delivery, and the bounded-liveness / fairness signals
around partial deadlock that Go's *global*-only detector misses (it caught 2
of 21 blocking bugs in the study).

### The guard-rail: any contract readable by a TS/Go dev in five minutes

Session types historically grow into monsters — `send`/`recv` becomes choice
becomes loop becomes a recursive protocol algebra, and a few years later
nobody uses it. The governing constraint of this RFC is a readability test
applied to every construct:

> Can any channel contract be explained to an ordinary TS/Go developer in five
> minutes? If understanding it requires a 40-page π-calculus paper, we lost.

So we keep the *mental model* of TLA+ (safety / liveness / fairness over a
trace) and leave its *machinery* out.

## Design

### Events and subjects

A contract is evaluated over a trace of events of the uniform form

```
subject.operation(payload)
```

- **operations** are `send`, `recv`, `close` — the observable channel actions;
- a **subject** is a *named* channel-like value — a channel, a done/cancel
  channel, a timer — whose operations produce the events.

So `work.recv(Job{id})`, `results.send(Result{id})`, `done.recv(_)`,
`out.close` are all events. This one form carries every clause below.

**An event subject is a named channel-like value whose operations produce
observable protocol events. Contracts may refer only to named subjects.**
Anonymous channel expressions are allowed in code but cannot be mentioned in a
clause — so a `select` arm that must participate in a protocol binds its source
first:

```aril
let deadline = time.after(time.seconds(1))     // named — usable in a contract
select { case <-deadline => … }
// not: select { case <-time.after(time.seconds(1)) => … }   // anonymous — unnameable
```

Naming the subject costs nothing, reads better in the code, and gives the
trace a stable identity. **Cancellation and timeout subjects must be named.**

A subject may carry an optional **role** — `cancel`, `timeout`, `signal` — a
semantic label used *only for diagnostics*, never a distinct event:

```aril
channel done role cancel
```

The event is still `done.recv(_)`; the role lets a violation read *"sent on
`out` after cancellation signal `done`"* instead of *"after `done.recv`"*.

Two properties keep the model teachable: only declared subject events are
observed — unrelated computation is invisible (TLA+'s stuttering-insensitivity,
without the name); and the contract constrains observable events, not the
implementation — a program may do more internally as long as its events
conform (refinement, in plain terms).

### Two levels: channel clauses and protocol clauses

**Channel clauses — local correctness.** A `channel <name> { … }` block states
invariants of one channel in isolation:

```aril
channel results {
  closed-by pool
  forbid send after close
  forbid recv after close
  never more than jobs.len() in flight
}
```

**Protocol clauses — cross-channel correctness.** The `contract` body declares
its subjects and states properties that *span* them, in one namespace, with
events qualified by subject:

```aril
contract WorkerPool {
  channel work
  channel results

  forbid results.send(Result{id}) before work.recv(Job{id})
  eventually results.close after work.close
}
```

The split mirrors the bug classes: local close-safety lives on the channel;
the interesting invariants — ordering, close-propagation, job→result pairing —
live one level up, where a clause can name two subjects at once. The `channel`
block stays (local invariants are real); everything relational moves up.

### Three kinds: safety, liveness, fairness

Every clause is one kind — the TLA+ division, in Go-readable form, each with an
honestly-stated runtime status.

**Safety — "bad things never happen".** The only *definitively* runtime-checked
kind: a forbidden event, when it occurs, is caught at that instant with blame.
`closed-by`, `forbid send after close`, `forbid recv after close`,
`forbid A before B`, `never more than N in flight`, `drains-before-scope-exit`
(the channel is closed and empty when its owning `scope` returns).

**Liveness — "good things eventually happen".** A function may break no safety
rule and still hang. `every work.recv(Job{id}) eventually results.send(Result{id})`,
`eventually results.close after work.close`. A monitor cannot *refute* a pure
"eventually" from a finite trace, so liveness is **runtime-checkable only in a
bounded / test mode** and reported as **non-definitive** — never as a proof.

**Fairness — "no participant is starved forever".** Kept in its most human
form — no weak/strong fairness, just "a `select` does not ignore one input
indefinitely": `fairness { no-starvation inputA }`. Fairness is
**observable/testable intent** (a stress run may search for starvation), not a
v1 proof obligation.

### Fan-out obligations

Some protocols require one event to be observed by *every* participant in a
declared set — not the ordinary "eventually one Y", but a **coverage
obligation** over a receiver set. A Go channel is by default a **work queue**
(each message goes to *one* receiver); **broadcast** (each message to *every*
receiver) is a different intent the contract must mark.

```aril
contract RateLimited {
  participant producer
  participant consumer
  channel deadline

  deadline delivered-to-all { producer, consumer }
}

contract PubSub {
  participant subscribers: Set<Subscriber>
  channel messages

  messages.send(m) delivered-to-all subscribers
}
```

`delivered-to-all` is **not** sugar over `eventually` — its check has a
different moment. At the protocol/scope boundary the expected receiver count is
known, so a missing receiver is definitive:

> **Fan-out obligations are discharged at the protocol/scope boundary. Missing
> receivers are definitive violations, not bounded-liveness guesses.**

This is what catches the one-shot-deadline footgun: `time.after` delivers to a
*single* receiver, so `delivered-to-all { producer, consumer }` is violated
definitively at scope exit. And because the contract marks **broadcast** intent
over a **work-queue** channel, Aril gives the teaching diagnostic *"you used a
one-shot / work-queue channel where the protocol requires broadcast delivery to
{producer, consumer}"* (E1209) — diagnosing the design error, not just the
symptom.

### The guard-rail — what we deliberately do *not* bring

The mental model is TLA+; the surface is Go. We take the safety / liveness /
fairness distinction, the trace-of-events model, and the fan-out coverage
idea, and we leave out everything that fails the five-minute test:

- temporal-logic operators (`[]`, `<>`, `~>`, `WF`, `SF`);
- state predicates with primed variables;
- a full action algebra;
- model-checking terminology in the surface language;
- a recursive protocol calculus / session-type algebra.

If a construct cannot be explained to a TS/Go developer in five minutes, it
does not enter the surface.

### Diagnostics

Grouped by kind. Safety and fan-out are definitive; liveness and fairness are
bounded/testable signals, reported as non-definitive.

- **Safety:** E1201 (close by a non-owner — `closed-by` violated), E1202
  (double close), E1203 (send after close), E1204 (recv after close), E1205
  (a `forbid A before B` ordering pattern violated), E1206 (capacity exceeded
  — `never more than N in flight`), E1207 (incomplete drain at the owning
  scope's exit).
- **Fan-out (definitive, at boundary):** E1208 (coverage: fewer than the
  declared participant set observed the event), E1209 (delivery-intent
  mismatch — a one-shot / work-queue subject used where broadcast is required).
- **Well-formedness:** E1210 (a clause names an anonymous or unbound subject —
  subjects must be named).
- **Liveness (bounded, non-definitive):** E1211 (a required `eventually` event
  not observed within the bound).
- **Fairness (testable, non-definitive):** E1212 (starvation of a declared
  participant observed under a stress run).

### Runtime — `arilrt` trace monitor

Under contract, each named subject lowers to a thin wrapper that appends its
`send` / `recv` / `close` events to a per-subject trace and evaluates the
declared clauses against it. Safety and well-formedness checks fire at the
offending event; fan-out coverage and drain are discharged at the owning
scope's boundary; liveness and fairness run only in the bounded / stress mode.
Blame is local and decentralized — a violation names the subject, the event,
and the goroutine/role (with the role label when present), in Aril coordinates
(D10). Modes panic / warn / stats / off and the elision-under-`off` story are
exactly RFC-0006's (no contract → no wrapper → byte-identical codegen). Count
and value relations on payloads (`sends == recvs`, "exactly N flow") remain a
value `ensures` (RFC-0006) on the draining function, not a channel clause.

## Alternatives considered

- **Declared session protocols / session-type algebra (Scribble / MPST).** A
  channel declares its full ordered protocol (`recv Job; send Result; choice …;
  loop …`), projected to per-endpoint monitors. Rejected as the surface model:
  it is the path that grows into a recursive protocol algebra and fails the
  five-minute test. We keep the trace model (forbidden / eventual / fair /
  delivered events qualified by subject), which expresses the high-frequency
  cases in English.
- **Infer-from-code static deadlock checking (Godel / Gong).** Infer a
  behavioural type from `scope`/`spawn`/channel code and model-check partial
  deadlock + liveness statically (Lange/Ng/Toninho/Yoshida). The strongest
  guarantee, zero surface — but a heavy implementation (an mCRL2-style
  checker), bounded analysis, and *static-first*, against the runtime-first
  stance. A future static path; the same trace clauses can later drive it.
- **One flat event alphabet with `cancel` / `timeout` as primitive events.**
  Rejected: a program holds concrete objects (`done: Channel<unit>`,
  `deadline: time.after`), and a flat alphabet cannot tell which is the cancel
  and which the timeout. Naming subjects (with optional `role` labels) gives
  the same expressiveness, better diagnostics, and trace identity, without
  magic events.
- **Extend RFC-0006 pre/post to channels.** Impossible by construction: pre/
  post/invariant are point-in-time state assertions over one call; they cannot
  see a *sequence* of events (RFC-0006 §Non-goals).

## Prior art

- **TLA+** (Lamport) — the source of the **safety / liveness / fairness**
  division and the **trace-of-events / stuttering / refinement** model we
  borrow. We take the mental model only; none of its temporal-logic syntax,
  primed-variable state predicates, or action algebra enters the surface.
- **Trace contracts** — Moy & Felleisen, "Trace Contracts" (JFP 2023): a
  predicate over an accumulated event trace — the formal shape used here.
- **Monitorability** — Leucker & Schallhart 2009; Havelund & Peled (RV 2023):
  safety is monitorable from a finite trace, pure liveness is not — the basis
  for "safety/fan-out are definitive, liveness/fairness are bounded/testable".
- **Runtime session monitoring** — Bocchi/Chen/Demangeon/Honda/Yoshida (FORTE
  2013 / TCS 2017): local per-endpoint monitors compose to a global guarantee
  — the blame model adopted here.
- **Empirical grounding** — Tu, Liu, Song, Zhang, ASPLOS 2019: message passing
  causes more blocking bugs than shared memory; channel misuse dominates.
- **Channel typestate** — typestate (Strom & Yemini 1986; Aldrich et al.,
  Plaid 2009): `Open → Closed`, `send` legal only while `Open` — the local
  close-safety subset, here generalised to a trace over named subjects.

## Transition / compatibility

Strictly additive. No existing program changes meaning; a channel with no
contract is lowered exactly as today (the Go runtime's existing close-panic
semantics are unchanged). Default mode for `run`/corpus is `panic`; liveness
and fairness clauses are evaluated only in a bounded / stress mode and always
reported as non-definitive. No deprecation window.

## History

- 2026-06-20 — created (`draft`).
