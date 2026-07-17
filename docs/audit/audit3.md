# Aril readiness audit — AUDIT-3 trap hunt

The **trap catalog**. Where AUDIT-2 ran the curated intuition sweep (4 models × 3
rungs × 10 tasks × N=3 = 720 cells) and found `compile == run` in *every* cell —
zero silent divergences *on the ten curated tasks* — AUDIT-3 does the thing the
curated sweep structurally cannot: it **adversarially hunts the silent quadrant**,
writing programs whose whole purpose is to compile-and-diverge, and it catalogs
what a developer arriving with TypeScript / Go / Rust priors will actually hit.

Measurement-first, per the AUDIT-0..3 charter: **no compiler change this epoch**.
Every entry is surfaced-not-remediated and carries an AUDIT-4 disposition
(teach / docs / diagnose(`hint`) / redesign(→D11) / fix-bug). The compiler *bugs*
the hunt turned up (D10 raw-Go leaks, a codegen crash, a nil-soundness hole) are
logged here and as backlog items; they are fixed in follow-up PRs, not mid-hunt.

## Method

- **Direct oracle sweep** — minimal `.aril` probes over silent-divergence
  candidates (numeric, containers/references, control/closures, strings/format),
  each run through the real `aril run` oracle (`--contracts=panic`), output
  compared against the TS/Go/Rust prior. Verified, not reasoned.
- **Adversarial subagent top-up** — four fresh probe subagents across the model
  tiers, each given the cheatsheet + `binding-surface.md` + the compiler and one
  domain, tasked to *write programs that compile but surprise*. Every reported
  find was re-run through the oracle here before it entered the catalog.
- **Classification (2×2):** *loud* (compiler rejects/diagnoses) vs *silent*
  (compiles + runs) × *honest-difference* (defensible, matches Go/Rust, needs a
  doc line) vs *silent-lie* (the familiar surface actively misleads). The
  dangerous quadrant — **silent × silent-lie** — is the direct test of the
  governing invariant *syntax must not lie*.

The headline result inverts AUDIT-2's reassuring `compile == run`: **the ten
curated tasks simply never exercised the surfaces where Aril lies.** Adversarial
probing found a dense silent-lie cluster, and its root is singular and
mechanical — **Go's lowering leaks through wherever a value is *rendered* or a
container/field is *defaulted***.

## The findings, frequency × severity ranked

### T1 — Printing any composite value leaks Go's `%v` [FLAGSHIP · silent-lie · very-high frequency]

`fmt.println(x)` and `"${x}"` interpolation of **any** composite render Go's
internal struct layout, not the Aril value:

| value | prints | a dev expects |
|---|---|---|
| `List<int>{1,2,3}` | `&{[1 2 3]}` | `[1, 2, 3]` |
| `Map<string,int>` `{a:1,b:2}` | `&{map[a:1 b:2] [a b]}` | `{a: 1, b: 2}` |
| `Set<int>{3}` | `&{map[3:{}] [3]}` | `{3}` |
| record `Point{x:1,y:2}` | `{1 2}` | `{x: 1, y: 2}` |
| tuple `(1,"a")` | `{1 a}` | `(1, "a")` |
| `Some(5)` / `None` | `{1 5}` / `{0 0}` | `Some(5)` / `None` |
| `Ok(5)` / `Err("boom")` | `{0 5 <nil>}` / `{1 0 boom}` (the `<nil>` is `Result<T,error>`'s empty `E`; `Result<T,string>` → `{0 5 }`) | `Ok(5)` / `Err("boom")` |
| sum `Circle(2.0)` / `Square(3.0)` | `{0 2 0}` / `{1 0 3}` | `Circle(2.0)` / `Square(3.0)` |

The sum-type / `Result` renderings are the worst: the leading integer is the
**variant tag**, and *every sibling variant's fields* are emitted, zero-padded and
interleaved (`Circle(2.0)` and `Square(3.0)` differ only in tag + which slot is
non-zero). `Err("boom")` → `{1 0 boom}` is not merely opaque — it looks like a
three-field record. Interpolation inherits it verbatim:
`"${l}"` → `&{[1 2 3]}`.

- **Why:** no `String()` / `Stringer` is generated for `Option`/`Result`/user sum
  types/records/tuples/`List`/`Map`/`Set`; the spec's true-but-misleading line
  ("each hole → one `fmt.Sprintf %v`, so any value type interpolates") promises
  legibility the raw `%v` dump does not deliver.
- **Class:** silent-lie — a semantics-leak / backend-divergence (`methodology.md`
  §11.6). A TS dev reaches for `console.log`/`${}` to debug *everything*; this is
  the first thing they do and it lies on the first composite they touch.
- **AUDIT-4:** the single highest-value fix — a generated `String()` for these
  types (diagnose/redesign). Eliminates the entire cluster at once.

### T2 — Omitting a reference-container field yields nil → SIGSEGV [silent-lie · soundness]

Constructing a record/class while **omitting** a `List`/`Map`/`Set` field
*compiles*, defaults that field to a raw Go nil pointer, and segfaults on first use:

```aril
type Wrap = { items: List<int>, n: int }
let w = Wrap{ n: 5 }        // compiles — partial construction is allowed
w.items.push(1)             // panic: nil pointer dereference (raw Go SIGSEGV)
```

Scalar fields zero-init safely (`0`, `""`); only reference-container fields are an
unsafe nil. The explicit `Wrap{ items: List<int>{}, n: 5 }` is fine.

- **Why it's the sharpest lie in the set:** it directly contradicts the
  cheatsheet's *headline* promise — *"There is no `null`, `nil`, or `undefined`."*
  The one invariant a newcomer is told to rely on is the one that segfaults, with
  a bare Go SIGSEGV (no Aril diagnostic).
- **Two coupled defects:** (a) partial construction is silently allowed; (b) a
  container's zero value is a nil, not an empty container.
- **AUDIT-4:** redesign — either reject partial construction of a
  container-bearing type (loud, E-code) or default container fields to an empty
  container (make the "no nil" promise true). Backlog + a follow-up PR.

### T3 — `spawn` silently captures an outer `var` by reference → unguarded data race [CO-FLAGSHIP · silent-lie · concurrency pillar]

```aril
var counter = 0
scope<unit, error> {
  for _ in 1..=1000 {
    spawn { counter = counter + 1; return Ok(()) }
  }
} catch e { return }
fmt.println(counter)                 // 955, 992, 990, 950 … never 1000
```

A `spawn` closes over the outer mutable `counter` by reference and races on it —
a natural-looking concurrent accumulator that silently produces a wrong,
nondeterministic answer. **No diagnostic, and `aril run`/`build` expose no `-race`
flag.** Rust's borrow checker rejects this outright; Go's `-race`/`go vet` flags
exactly this pattern; Aril, whose *uncolored concurrency* is a headline feature
(cheatsheet §9), ships the sharpest edge unguarded.

- **Class:** silent-lie at the concurrency pillar — the co-flagship with T1
  because both hit a core selling point (T1 = Option/Result rendering, T3 =
  uncolored concurrency) and both produce wrong output with zero signal.
- **AUDIT-4:** the heaviest open question. Options span a spectrum: surface a
  `-race` build mode (cheap, Go already supports it — at least make the race
  *detectable*); a `hint` on a `var` captured-and-mutated inside `spawn`; or a
  deeper capture-discipline (`spawn` may only *read* outer bindings, mutation goes
  through a channel/atomic — the structured-concurrency direction, D31/D52). Note
  the contrast: Aril already enforces structured *scope join*; it does **not**
  enforce capture safety. redesign, couples to the concurrency roadmap.

### T4 — `0.1 + 0.2` prints `0.3` on literals, `0.30000000000000004` on variables [silent-lie]

```aril
fmt.println(0.1 + 0.2)                 // 0.3
let a = 0.1; let b = 0.2
fmt.println(a + b)                     // 0.30000000000000004
```

Byte-identical arithmetic, two answers: literal operands const-fold with Go's
arbitrary-precision untyped constants (the exact nearest float64 to true 0.3);
variables do IEEE-754 at runtime. TS is identical in both cases (`…004`). The
canonical "floats are weird" demo silently *lies* on literals — reinforcing a
false mental model, and giving a spot-check with literals false confidence.

- **Class:** silent-lie (self-inconsistent surface). **AUDIT-4:** docs/teach; a
  gotcha row. Low remediation cost, notable because Aril is otherwise strict
  about numeric types (see the loud-and-good list).

### T5 — Map/Set iteration is insertion-ordered, but the spec says "unspecified — matches Go" [silent · latent-trap · doc-vs-impl]

`for (k,v) in m` and `for x in s` iterate in **insertion order, deterministically**
(verified stable across repeated runs) — the runtime is a hand-rolled ordered
wrapper (`{ m map[K]V; order []K }`). But `language-spec.md` (§Maps/§Sets)
explicitly states *"order unspecified — matches Go"*. A program that works
reliably today is silently depending on behaviour the spec disclaims — a landmine
for anyone who later "fixes" `Map` to a thin Go map to honor the spec.

- **Note the pleasant half:** the *impl* protects the TS/JS prior (insertion
  order), which is the right call. The bug is the **spec text**, which is stale
  and dangerous.
- **AUDIT-4:** redesign/spec — commit the spec to insertion-order (make the safe
  behaviour a guarantee) or the divergence is a latent trap. D11/D14-adjacent.

### T6 — Record value-copy is *shallow*: reference-typed fields stay aliased [silent-lie · partial]

`var b = a` on a record copies scalar fields (independent — verified) but leaves
`List`/`Map`/`Set` fields **aliased** (both copies observe each other's pushes).
"Records are value types" (cheatsheet) reads as a stronger guarantee than holds;
the shallow-copy interaction with reference fields is undocumented.

- **Class:** silent-lie (partial). **AUDIT-4:** docs (spell out shallow-copy) —
  deep-copy-on-assign would be a heavy semantic change, so likely a teach, not a
  redesign.

### T7 — `.recv()` on a drained/closed channel returns the zero value silently [silent · honest-diff-from-Go]

```aril
ch.send(42); ch.close()
ch.recv()   // 42
ch.recv()   // 0  — no signal that the channel is closed
```

Bare `.recv()` gives no closed-signal (unlike Go's `v, ok := <-ch`); the honest
form is `for v in ch { … }`, which ends on close. A loop of `.recv()` silently
yields infinite zeros. Matches Go's value-half, drops Go's `ok`-half.

- **AUDIT-4:** docs + consider a `.recv(): Option<T>` (redesign) so close is
  representable — couples to the concurrency surface.

### T8 — `[]T` index-assignment `s[i] = v` mutates (and aliases the backing array) even through `let` [silent · honest-diff · undersold]

The cheatsheet calls `[]T` a "value view — pure accessors only, no `.push`", but
`s[i] = v` is legal and mutates the shared backing array — through a `let`
binding, and through a sub-slice `full[0:2]` that aliases `full`. Two mismatches:
the cheatsheet undersells (index-assignment is *not* pure), and `[]T` allows
`s[i]=v` while `List` *forbids* `l[i]=v` (requires `.set`) — an inconsistency.

- **AUDIT-4:** docs (a trap-table row) + note the `[]T`/`List` index-write
  asymmetry for the container-model owner.

### T9 — Integer division truncates; overflow/underflow/narrowing wrap silently [silent · honest-diff-from-TS]

All match Go/Rust, diverge from TS's float `number`; none is in the cheatsheet
trap table:

- `5 / 2` → `2` (TS `2.5`). The single most common TS-dev arithmetic surprise —
  **absent from the trap table.**
- `int8` `127 + 1` → `-128`; `int64` max `+ 1` → min; `uint` `0 - 1` →
  `18446744073709551615`.
- narrowing `int8(300)` → `44`; `int(2.9)` → `2` (truncates, not rounds).

- **AUDIT-4:** docs — add `5/2 → 2` and a one-line "fixed-width integers wrap" to
  the trap table.

### T10 — Float printing uses Go `%g`: `1000000.0` → `1e+06`, `1.0` → `1` [silent · honest-diff-from-TS/Rust]

`fmt.println(1000000.0)` → `1e+06`; `1234567.5` → `1.2345675e+06`; `1.0` → `1`
(indistinguishable from an int). TS and Rust default to plain decimal well past
this. Matches Go `%v`/`%g` exactly.

- **AUDIT-4:** docs (surprising for a TS-onboarding page) — subsumed if T1's
  formatter work touches scalar float rendering.

### T11 — Integer `10 / 0` is a runtime panic (raw Go); TS gives `Infinity` [silent-at-compile · honest-diff-from-TS]

`10 / 0` compiles, then `panic: runtime error: integer divide by zero` (Go
goroutine trace; the `.aril` coordinate maps but the message is raw Go). `1.0/0.0`
→ `+Inf` (no panic). A TS dev expects `Infinity` for both. Matches Go/Rust
(panic). *Constant* `10/0` is a compile error but with **raw Go phrasing**
("division by zero", `# aril-output` header) — a D10 brush.

- **AUDIT-4:** docs + the runtime-panic-text D10 question (see the compiler-bug
  section).

### T12 — String `.len()` is bytes; `for c in s` yields runes that print as integers [silent · honest-diff · partly documented]

`"héllo".len()` → `6` (bytes; TS UTF-16 `.length` is `5`). `for c in s` iterates
**5 runes**, but each `c` is a `rune` and prints as its **codepoint integer**
(`104 233 108 108 111`), not the character — you need `string(c)` to print `é`.
Byte-indexing is documented; the *iterate-yields-printable-ints* half is not.

- **AUDIT-4:** docs (the `for c in s` → codepoint-int surprise; couples to T1 if
  `rune` gets character-rendering).

### T13 — Bare `Map[k]` index returns the zero value on a miss [silent · honest-diff · already-documented, design-Q open]

`m["absent"]` → `0`, indistinguishable from a real `.set("absent", 0)`. Already in
the cheatsheet trap table (`m.has`/`m.get` are the honest forms); re-confirmed. The
open **design question** the AUDIT-2 hand-off flagged: *should a bare `m[k]` index
exist on `Map` at all*, or should the only index be the safe `.get(k): Option<V>`?
Removing bare-index would make the miss unrepresentable-as-zero.

- **AUDIT-4:** redesign candidate (remove bare `Map` index) vs teach (keep, it's
  documented). D11.

### T14 — `math.round`/`math.trunc` resolve but are undocumented, with silent Go semantics [silent · doc-incompleteness]

`math.round(-2.5)` → `-3` (Go round-half-away-from-zero) where JS
`Math.round(-2.5)` is `-2` (half-toward-+∞). `math.round`/`math.trunc`/`math.max`
resolve and run, but `binding-surface.md`'s math section lists only
sqrt/abs/pow/log*/floor/ceil/min/max/pi — so the bound set is broader than
documented (yet `math.cbrt`/`strings.equalFold` are *not* bound → clean E0217).
The bound surface is partly-curated, partly-under-documented; an undocumented
member silently carries raw Go semantics.

- **AUDIT-4:** docs — reconcile `binding-surface.md` with the actual bound set
  (which the bindgen registry is the source of truth for); map which exports
  resolve. Low severity, but a self-service/discoverability gap.

### T15 — `fmt.printf` verb mismatch silently corrupts output [silent · honest-diff-from-Rust · low]

`fmt.printf("%d\n", "hello")` → `%!d(string=hello)` (Go's format-error marker, no
compile error, no runtime error). Rust `println!` is compile-time verb-checked.
Low frequency (interpolation `${}` is the taught path).

### T16 — A discarded `Result` is silently dropped (no must-use) [silent · honest-diff-from-Rust · lie-vs-positioning]

`strconv.atoi("x")` as a bare statement compiles and runs; the `Err` vanishes and
the program continues. Matches Go (a value may be ignored), but the language's
whole positioning is *"no exceptions — `Result<T, E>`"* (cheatsheet §5), and Rust
makes `Result` `#[must_use]`. An unhandled failure disappearing silently
undercuts the Result-first promise. (Note: `E0215` used to police a *discarded
slice `.push`*; there is no analogue for a discarded `Result`.)

- **AUDIT-4:** diagnose candidate — a `hint`/warning on a discarded `Result`
  expression-statement (the must-use discipline, opt-in with the `hint` tier).

### T17 — `defer` inside a loop accumulates to *function* exit, not per-iteration [silent · honest-diff · undersold]

```aril
for i in 0..3 { defer fmt.println("d", i); fmt.println("b", i) }
// b0 b1 b2 (after loop) d2 d1 d0    — all defers fire at once at function exit
```

Matches Go (documented "runs at function exit, LIFO"), but a TS (`finally`) or
Rust (block-scoped `Drop`) dev writes `defer` in a loop body expecting
per-iteration cleanup and instead accumulates N deferred calls firing together at
the end (a real resource-lifetime bug — N files/locks held open until return).

- **AUDIT-4:** docs — the cheatsheet's `defer` line says "function exit"; a
  trap-table row on the loop-body misuse would help the TS/Rust half.

### T18 — `defer` is skipped by `os.exit`, runs on `panic` [silent · honest-diff · undocumented]

A `defer`ed cleanup never runs if the function exits via `os.exit(code)` (Go
`os.Exit` skips defers); it *does* run on `panic`. Cross-language-consistent with
Go/Rust, but Aril's own `defer` docs give no exit-bypass caveat — a file-close or
lock-release `defer` silently no-ops on an `os.exit` path.

- **AUDIT-4:** docs (one caveat line).

### T19 — `unwrapOr(fallback)` evaluates its argument eagerly [silent · honest-diff-from-Rust · lie-vs-TS `??`]

`Some(5).unwrapOr(expensive())` runs `expensive()` even though the receiver is
`Some` and the value is discarded. Matches Rust exactly (which is *why* Rust ships
a separate `unwrap_or_else` for laziness); diverges from a TS dev's `??`/`?.`
short-circuit intuition — any side-effecting or costly fallback always runs.

- **AUDIT-4:** docs; a lazy `unwrapOrElse(() => …)` is the eventual mirror (folds
  into the Option/Result combinator-lowering path when a further combinator lands).

## Prior-leakage cluster (the AUDIT-2 hand-off, re-confirmed)

The Go-idiom spellings remain **loud** (clean `E0217`), so they are low-severity
teach/docs, not silent traps — but they are the highest-frequency *misses*:

- `sort.Slice`/`sort.slice` → `sort.sorted(xs, less)` — the dominant AUDIT-2 miss;
  AUDIT-4's flagship `hint`-tier remediation (a tailored E0217 "did you mean").
- `strings.Fields` → `strings.fields`, `strings.EqualFold`→ (unbound), and the
  general casing rule (Aril lowercases the Go export's first letter). Clean E0217.

## Compiler bugs surfaced (D10 leaks / crashes — → follow-up PRs, backlog)

These are not intuition traps; they are defects the hunt exposed. Logged as
backlog items, fixed outside the measurement epoch (the F-catch-discard precedent).

1. **Unused local variable → raw Go `x declared and not used`** (no E-code,
   `# aril-output` header, `../../m.aril` path). D10 violation on a trivial,
   common mistake.
2. **Same-block / bare-`{}`-block shadow → raw Go `x redeclared in this block`**
   (+ leaks a `gen/main.go` secondary location). Shadowing inside `if {}`/`for {}`
   works; a bare `{}` block emits no nested Go scope, so a shadow collides. D10 +
   a narrow codegen scoping gap. (Whether same-block *re*-declaration should be
   allowed at all is a separate design call; the *leak* is the bug.)
3. **Uninitialized local container var → internal crash.** `var l: List<int>`
   (no initializer) → `aril: codegen: unhandled expression <nil>` — no E-code, no
   coordinate. A compiler-internal panic surfaced as a user-facing error.
4. **`for x in rec.listField` fails to compile** (`cannot range over h.Items …
   *arilrt.List[int]`) while `for x in l` over a local/param works — the
   non-`Ident` container-receiver leak already in the backlog (CONTAINER-MODEL
   carry-forward), now confirmed reachable through a `for`-loop over a field.
5. **Runtime panics carry raw Go text** (div-by-zero, index-out-of-range,
   nil-deref): the `//line` coordinate maps to `.aril`, but the message + the
   goroutine trace are Go. A D10 question at the *runtime* boundary
   (Open-Problem-#3, panic semantics) rather than the compile boundary.

## Non-traps confirmed (record as *safe* — the audit's reassurances)

- **Map/Set iteration is deterministic + insertion-ordered** (impl; see T5 for the
  spec-text caveat) — *safer* than Go's randomization, protects the TS prior.
- **Per-iteration loop-variable capture:** `for i in 0..3 { fns.push(() => i) }`
  → `0 1 2` (like Rust / Go ≥1.22 / TS `let`), not the legacy-JS `3 3 3`.
- **Strict numeric typing (loud-and-good):** `1 + 2.0` → E0201; `float64 = 5/2`
  → E0201 (Aril rejects Go's classic `var x float64 = 5/2` gotcha); `int` vs
  `int64` compare → E0201. Aril is *stricter* than Go here — builds trust.
- **`==`** compares tuples/records field-wise; class instances → E0401 (→ `refEq`)
  — documented, loud.
- **Reversed range** `for i in 5..2` → empty (0 iterations), no panic.
- **`l.get(i)` / `l.toSlice()`** behave honestly (`None` on OOB; `toSlice` is a
  genuine independent copy).
- **`&&` / `||` short-circuit** correctly (side-effect-probed); **`.map` on
  `Err`/`None` short-circuits** without invoking the closure.
- **`match` misuse is loud:** a wildcard `_` arm before other arms → E0304
  ("unreachable arm"); a missing case → E0303. Caught, not silent.
- **`send` on a closed channel** panics immediately (loud crash) — raw Go text
  (`send on closed channel`), so it joins the runtime-panic-text D10 question, but
  it is not a *silent* divergence.

## Verdict and hand-off to AUDIT-4

AUDIT-2's `compile == run` was true *and* incomplete: the curated corpus never
rendered a composite, never omitted a container field, never printed a float
literal, never raced on a captured `var`. **The silent-lie surface is real and
concentrates at three boundaries — value *rendering* (T1), container/field
*defaulting* (T2), and unsynchronized *capture* (T3) — each a spot where Go's
lowering shows through a familiar-looking surface.** Ranked remediation for
AUDIT-4:

1. **Generated `String()` for composites (T1, T10-scalar, T12-rune).** One fix
   collapses the largest, highest-frequency silent-lie cluster. *diagnose/build.*
2. **The `spawn` capture-race (T3).** At minimum surface a `-race` build mode so
   it is *detectable*; ideally a `hint` on a mutated captured `var`, or a
   capture-discipline. *redesign, couples to the concurrency roadmap.*
3. **The nil-container-field soundness hole (T2).** Reject partial construction or
   default containers to empty — make "no nil" true. *redesign + bug.*
4. **Reconcile the Map-order spec text (T5)** and settle the bare-`Map[k]` design
   question (T13). *spec/redesign, D11/D14.*
5. **The compiler-bug backlog** (unused-var / shadow-leak / uninit-crash /
   range-over-field / runtime-panic-text). *fix PRs, D10.*
6. **Docs bucket** (T4, T6, T7, T8, T9, T11, T12, T14, T16–T19; the `sort.sorted`
   hint): trap-table rows + the gotchas page + the AUDIT-4 `hint` tier for `sort`
   and the discarded-`Result` must-use.

The governing invariant *syntax must not lie* holds at the **type** boundary
(Aril is strict, loud, and often safer than Go — insertion-ordered containers,
per-iteration capture, `float64 = 5/2` rejected) and breaks at the **value-
rendering**, **container-defaulting**, and **concurrent-capture** boundaries. That
is a narrow, mechanical, largely fixable root — the reassuring shape for a pre-v1
finding, and exactly the class of thing the pre-v1 audit exists to catch while the
cost is a doc + a compiler pass, not an ecosystem migration.
