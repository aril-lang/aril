| Field | Value |
|---|---|
| RFC | 0011 |
| Title | Native panic traces (`aril explain`) |
| Status | reserved |

# RFC-0011 — Native panic traces (`aril explain`)

**This number is reserved; the RFC is not yet written.**

The mechanism — a supported translator that reframes a Go panic block into a
native Aril trace (message table + a per-binary symbol sidecar + a frame policy),
exposed post-hoc as `aril explain` (with `aril guard` and an in-process `arilrt`
`recover` as later surfaces) — is being **explored first** as a design note at
[`docs/aril-explain.md`](../aril-explain.md). Once the shape is validated in
practice, that note graduates into this RFC.

`reserved` is a pre-`draft` placeholder (it is not one of the process's lifecycle
states `draft | accepted | implemented | superseded`, see
[`0000-process.md`](0000-process.md)) — it only claims the number so it is not
reused while the mechanism is being played with.

**Context:** the intermediate remediation for the AUDIT-3 compiler-bug *"runtime
panics carry raw Go text"* — it fixes the *presentation* of a runtime panic
without settling panic *semantics* (the separate open question).
