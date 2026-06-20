# RFC-0007 — Channel typestate & scope-completion contracts

| Field | Value |
|---|---|
| Number | 0007 |
| Status | draft |
| Created | 2026-06-20 |
| Supersedes | — |
| Target | `lang-spec/grammar.ebnf` (the `channel`-clause in a `contract` block), `lang-spec/type-system.md` (T-Chan-Closer, T-Chan-Drain), `lang-spec/builtins.md` (`Channel`/`SendChan`/`RecvChan` contract surface), `lang-spec/diagnostics.md` (E12xx channel codes), `lang-spec/lowering-go.md` (§ChannelContractIR — runtime typestate wrapper + scope-exit drain check); `examples/README.md` + RFC-0004 corpus metadata; `docs/rfcs/README.md` index row |

## Summary

Add optional, runtime-checked **channel contracts** — a per-channel
**typestate** (a declared single closer + send-after-close / double-close
checking) and a **scope-completion** obligation (the channel drains and
closes cleanly before its owning `scope` exits) — plus an explicitly
**second-class, bounded-timeout** liveness proxy (`within <dur>`). They are
declared in the same separable `contract` block as RFC-0006, share its four
modes (panic / warn / stats / off) and its `arilrt` runtime layer, and report
in Aril source coordinates (D10).

This is the **trace/session-contract branch** RFC-0006 deferred in its
Non-goals: a *different mechanism* from pre/post/invariant — those are
point-in-time state assertions and cannot see ordering, protocol, or
liveness. RFC-0007 targets precisely the channel value/close-protocol bugs
that Aril's structured concurrency, the Go runtime, and `-race` do **not**
already cover. Scope is deliberately minimal — channel typestate +
completion, **not** a declared session-protocol sub-language (deferred).

## Motivation

### The residual that nothing else covers

Empirically, message passing is *more* blocking-bug-prone than shared memory
in real Go systems — of 85 blocking bugs studied across Docker / Kubernetes /
etcd / CockroachDB / gRPC-go / BoltDB, channel misuse (missing send/recv,
premature/erroneous close) is the single largest cause (Tu et al., ASPLOS
2019). The fixes are tiny and local (~7 lines on average) — i.e. *locally
specifiable*, exactly what a contract can state.

Aril already disposes of much of the concurrency-bug space *by construction*,
so RFC-0007 must target only what remains:

- **Goroutine leaks / orphaned tasks / un-cancelled context** — eliminated by
  structured concurrency: a `spawn` cannot outlive its `scope`, which joins
  all children. No contract needed.
- **Global deadlock** (all goroutines asleep) — the Go runtime already panics.
- **Data races** — `go test -race` already covers these (a data race is *not*
  a channel-contract concern; leave it there).

What is left — and what RFC-0007 targets — is the **channel value/close
protocol**, ranked by real-world frequency:

1. **Close-safety:** send-on-closed, double-close, close by a non-owner,
   send-after-close. The Go runtime only *panics at the symptom*; nothing
   prevents it or attributes it in Aril terms.
2. **Producer/consumer completion:** producer closed early / consumer expected
   more; counts that don't reconcile. Structured concurrency turns this into a
   *hang*, not a prevention.
3. **Partial deadlock** (a subset of goroutines stuck while others run) — the
   exact blind spot of Go's global-only detector (which caught 2 of 21
   blocking bugs in the study).

### Same strategic bet as RFC-0006

A channel contract is an **executable specification** of a channel's protocol.
On the corpus it deepens `run_ok` for the concurrency examples (a protocol
violation becomes a run failure); for code agents it is an oracle for the
hardest-to-debug class — a wrong close or an early producer exit becomes a
precise, blamed, source-located error instead of a panic three goroutines
away or a silent hang.

## Design

### The monitorability boundary (the load-bearing constraint)

A runtime monitor is fundamentally a **safety detector**: from a finite
execution it can *definitively* catch "a bad thing happened" (a send after
close, a wrong closer, an incomplete drain at scope exit) the instant it
happens. It can **never** refute a pure **liveness** claim ("the channel is
*eventually* drained", "the system makes progress") from a finite trace — a
blocked goroutine simply hasn't reached the next event yet (Leucker &
Schallhart 2009; Havelund & Peled 2023). Go's own detector proves the point:
it only fires when *every* goroutine is asleep, never for partial deadlock.

RFC-0007 therefore splits every obligation into two tiers, and the surface
keeps them visibly distinct:

- **Safety (definitive, first-class):** closer ownership, double-close,
  send-after-close, drain-complete-at-scope-exit. Checked exactly; a violation
  is a real error with blame.
- **Liveness (bounded proxy, second-class, opt-in):** partial-deadlock / leak,
  via a `within <dur>` timeout. A timeout is reported as **non-definitive** —
  "no progress within the bound", *not* "deadlock proven". Never on by default.

### Surface — a `channel` clause in the `contract` block

Channel obligations attach to the channel's binding name inside the
`contract` block of the function (or scope) that creates it — reusing
RFC-0006's separable block, no new top-level form:

```aril
func runPool(jobs: []Job): []JobResult {
  let work    = makeChannel<Job>(jobs.len())
  let results = makeChannel<JobResult>(jobs.len())
  scope {
    spawn feed(work, jobs)          // the producer
    spawn pool(work, results)       // workers
  }
  return drain(results)
}

contract runPool {
  channel work {
    closed-by feed            // exactly one closer; close elsewhere is E1201
    no-send-after-close       // send once `feed` has closed is E1203 (default on)
  }
  channel results {
    closed-by pool
    drains-before-scope-exit  // results fully drained/closed before the owning
                              // scope returns, else E1205 (producer-closed-early
                              // / consumer-expected-more)
  }
}
```

Clauses (all optional, each a single safety obligation):

- **`closed-by <site>`** — names the single closer (a `spawn` target, a
  function, or `self`). Any `close()` from elsewhere is **E1201**; a second
  close is **E1202**.
- **`no-send-after-close`** — a `send` after the declared closer has closed is
  **E1203**. (This is the default for any channel under contract; the clause is
  explicit documentation.)
- **`drains-before-scope-exit`** — at the owning `scope`'s exit, the channel
  must be closed and its buffer empty; otherwise **E1205** (the producer left
  data no consumer took, or closed before the consumer's expected count). This
  is a *safety* check at a sound checkpoint — `scope` exit is a real boundary
  control always reaches.
- **`within <dur>`** (liveness proxy, opt-in) — bound a blocking op or the
  scope; on timeout, **E1206**, reported as a *non-definitive bounded-liveness*
  signal, never as a deadlock proof.

### Module — runtime: `arilrt` channel wrapper

Under contract, a `Channel<T>` lowers to a thin wrapper tracking typestate:
`{ open bool, closerID, sends, recvs }`. Each `send` / `recv` / `close`
checks-and-transitions before delegating to the real Go channel:

```
func ChanClose(mode Mode, c *CChan, byID Site, v Violation)  // closer-owner + double-close
func ChanSend(mode Mode, c *CChan, v Violation)              // send-after-close
func ChanDrain(mode Mode, c *CChan, v Violation)             // at scope exit
func ChanWithin(mode Mode, c *CChan, d Duration, v Violation) // bounded liveness proxy
```

Blame follows the decentralized session-monitor result (Bocchi et al. 2013):
purely *local* per-channel checks compose into a global guarantee, so a
violation names the offending channel, the operation, and the goroutine/role
— in Aril coordinates (D10). Modes panic / warn / stats / off and the
elision-under-`off` story are exactly RFC-0006's (no contract → no wrapper →
byte-identical codegen).

### Corpus integration

The concurrency examples (`worker_pool`, `pipeline`, `concurrency`) gain
channel contracts and run under `--contracts=panic` in the run pass; a
protocol violation is a **run failure** (RFC-0004 / D25), the same deepening
of `run_ok` RFC-0006 introduced. The `rate_limited` deadlock — uncatchable by
any RFC-0006 contract — is now reachable only by an opt-in `within` proxy, and
the RFC is explicit that this is a bounded approximation, not a proof.

## Alternatives considered

- **Declared session protocols (Scribble / MPST-style).** A channel declares
  its full protocol (`recv Job; send Result; repeat until close`), projected to
  per-endpoint monitors — catching ordering and protocol fidelity, not just
  close-safety. Rejected for v1: it is a protocol-declaration *sub-language*,
  the open-ended surface-growth RFC-0006 deliberately avoided. The chosen
  typestate is the high-frequency subset (close/completion) at a fraction of
  the surface; sessions can be a later RFC, and the `closed-by`/`drains`
  obligations are already a partial behavioural type it can build on.
- **Infer-from-code static deadlock checking (Godel / Gong).** No annotations:
  infer a behavioural type from `scope`/`spawn`/channel code and model-check
  partial deadlock + liveness at compile time (Lange/Ng/Toninho/Yoshida). The
  strongest guarantee, zero surface — but a heavy implementation (an mCRL2-
  style checker), bounded analysis, and *static-first*, against the project's
  runtime-first stance. Deferred to a future static-discharge path; the channel
  contract is designed so the same `closed-by`/`drains` facts can later drive a
  static checker (one declaration, two backends).
- **Extend RFC-0006 pre/post to channels.** Impossible by construction: pre/
  post/invariant are point-in-time state assertions and cannot express
  ordering/protocol/liveness (RFC-0006 §Non-goals). RFC-0007 is a *different
  mechanism* (typestate over a trace), deliberately a separate RFC.
- **Rely on the Go runtime + `-race`.** They cover the wrong things — global
  deadlock only, and data races only — leaving the entire channel value/close
  protocol (the largest blocking-bug source) uncovered.

## Prior art

- **Empirical grounding** — Tu, Liu, Song, Zhang, "Understanding Real-World
  Concurrency Bugs in Go," ASPLOS 2019: message passing causes more blocking
  bugs than shared memory; channel misuse dominates; fixes are small/local.
- **Channel typestate / linearity** — typestate (Strom & Yemini 1986; Aldrich
  et al., Plaid 2009): legal operations depend on object state; `Open → Closed`
  with `send` legal only in `Open`. Linear/affine channel endpoints (Rust
  `session_types`, GoPi's linear channels in Go) make "use once / don't reuse"
  a type obligation.
- **Trace contracts** — Moy & Felleisen, "Trace Contracts" (JFP 2023): a
  predicate over an accumulated event trace; the model for any cross-event
  obligation beyond a single state.
- **Runtime session monitoring** — Bocchi/Chen/Demangeon/Honda/Yoshida,
  "Monitoring Networks through Multiparty Session Types" (FORTE 2013 / TCS
  2017): a protocol projected to per-endpoint runtime monitors; *local checks
  compose to a global guarantee* (the blame model adopted here).
- **Monitorability** — Leucker & Schallhart 2009; Havelund & Peled (RV 2023):
  safety is monitorable, pure liveness is not — the basis for the safety/
  bounded-proxy split.
- **Go-specific static verification** — Ng & Yoshida (CC 2016); Lange/Ng/
  Toninho/Yoshida "Fencing off Go" (POPL 2017) and Godel (ICSE 2018): infer
  behavioural types from Go channel code, check global *and partial* deadlock
  and liveness statically. The future static path; structured concurrency makes
  their "fencing" boundedness condition easy to satisfy.

## Paired edits

On acceptance, the implementing PRs touch:

- `lang-spec/grammar.ebnf` — the `channel <name> { … }` clause in a `contract`
  block; the `closed-by` / `no-send-after-close` / `drains-before-scope-exit` /
  `within` sub-clauses.
- `lang-spec/type-system.md` — T-Chan-Closer (single-closer well-formedness),
  T-Chan-Drain (drain obligation references a channel of the owning scope).
- `lang-spec/builtins.md` — the contract surface over `Channel`/`SendChan`/
  `RecvChan` (elevating the existing "closing a closed channel panics" line
  into a checked contract).
- `lang-spec/diagnostics.md` — E1201 (close by non-owner / multiple closers),
  E1202 (double close), E1203 (send after close), E1204 (channel clause names
  an unknown channel/site), E1205 (incomplete drain at owning-scope exit),
  E1206 (`within` bounded-liveness timeout — non-definitive).
- `lang-spec/lowering-go.md` — §ChannelContractIR: the typestate wrapper, the
  scope-exit drain check, the `within` timeout proxy, four-mode dispatch.
- RFC-0004 corpus metadata (`example.toml`) — channel-contract dimension on
  the concurrency examples; `examples/README.md` note.
- `docs/rfcs/README.md` — index row.

Atomic fixtures (hard rule) accompany each new construct and E-code in
`tests/{grammar,sema,codegen}/`.

## Transition / compatibility

Strictly additive. No existing program changes meaning; a channel with no
contract is lowered exactly as today (the Go runtime's existing close-panic
semantics are unchanged). With contracts, default mode for `run`/corpus is
`panic`. No deprecation window.

## Open questions

1. **Closer-site granularity.** `closed-by feed` names a `spawn` target; is a
   finer grain (a specific call site, a role parameter) needed, or is
   function/spawn-site enough for v1? *Start coarse.*
2. **`drains-before-scope-exit` for unbounded streams.** A channel intended to
   outlive a scope (a long-lived service bus) does not fit the drain-at-exit
   model. v1 scopes the obligation to channels *owned by* a `scope`; channels
   that escape are out of scope (and arguably a structured-concurrency smell).
3. **Count contracts.** "exactly N items flow" / "sends == recvs" is a natural
   completion obligation but needs the value-accounting of RFC-0006 (a counter
   `ensures`). Whether to express it here or via an RFC-0006 `ensures` on the
   draining function. *Deferred — likely RFC-0006.*
4. **Declared session protocols** (ordering/fidelity) — the deferred Scribble/
   MPST branch. A later RFC; the `closed-by`/`drains` facts are forward-
   compatible with it.
5. **Static discharge** — re-projecting the channel contract to a Godel/Gong-
   style static partial-deadlock checker (one declaration, two backends).
   *Deferred; the surface is shaped to allow it.*
6. **`within` default duration / units** and whether it belongs on the channel,
   the op, or the `scope`. *Opt-in only in v1; no default.*

## History

- 2026-06-20 — created (`draft`). Scope chosen (channel typestate +
  scope-completion; declared sessions and static inference deferred) after a
  three-axis research pass (Go session types, runtime monitoring /
  monitorability, real-world Go concurrency bugs). Sibling to RFC-0006; takes
  up its §Non-goals concurrency branch.
