# Atomics and lock-free data structures — design doc

Forward-looking design for binding Go's `sync/atomic` and writing **honest
lock-free** containers in Aril (a Treiber stack, a bounded ring buffer, RCU
ordered sets). This is a design sketch, not yet implemented: it is the target
surface a set of aspirational `target-sketch` corpus examples are written
against, and the acceptance target for the generics and atomics-binding work
that implements it.

## The load-bearing insight — GC is the RCU grace period

Aril compiles to Go, so the garbage collector *is* the reclamation mechanism a
lock-free structure would otherwise hand-write. A reader that `load()`s the
current reference keeps that version reachable for as long as it holds it; a
writer publishes a new version with `compareAndSwap` and drops the old one. No
hazard pointers, no epoch/QSBR machinery, nothing from `unsafe` or `runtime` —
`sync/atomic` + the GC is the whole toolkit. This is what makes lock-free in
Aril honest, and in fact simpler than in a manually-managed language.

## The surface

### Scalar atomics — constructable value-handles

`atomic.Int64`, `atomic.Uint64`, `atomic.Bool` are ordinary constructable
value-handles (the `sync.Mutex` machinery — `atomic.T{}` → Go `atomic.T{}`,
methods pointer-receiver). No generics; a pure binding-table addition.

```aril
import atomic                      // → sync/atomic

let counter = atomic.Int64{}       // zero-valued, ready to use
counter.add(1)                     // : int64  (new value)
let n = counter.load()             // : int64
counter.store(0)                   // : unit
let prev = counter.swap(5)         // : int64
let won = counter.compareAndSwap(5, 6)   // : bool
```

Method set: `load` / `store` / `swap` / `compareAndSwap` for all three, plus
`add` for the integer types. Scalar atomics alone suffice for a lock-free ring
buffer over atomic head/tail indices.

### Atomic references — `atomic.Pointer<T>`

Aril has no raw pointers: a `class` instance is a reference and "nil" is modelled
as `Option`. So the atomic cell is typed over a class `T`, and its load is an
`Option<T>` (`None` = the nil pointer):

```aril
class Node<T> {
  let value: T
  let next:  Option<Node<T>>       // immutable link — RCU copies, never mutates
}

let head = atomic.Pointer<Node<int>>{}          // starts empty (holds None)
head.store(Node<int>{ value: 1, next: None })   // publish a reference
let top = head.load()                            // : Option<Node<int>>  (lock-free)
let won = head.compareAndSwap(old, next)         // : bool
let prev = head.swap(None)                        // : Option<Node<int>>
```

Method set: `load(): Option<T>`, `store(v: T): unit`, `swap(v: Option<T>):
Option<T>`, `compareAndSwap(old: Option<T>, new: Option<T>): bool`. CAS compares
by reference identity (Go pointer equality), not structurally — RCU swaps the
*identity of the published version*.

`atomic.Pointer<T>` is generic. The credible implementation is a first-class
generic type modelled like `Map`/`Set` (a `sema` type + inference + an `arilrt`
wrapper over `atomic.Pointer[T]` + container-style codegen), **not** a
flat-string handle-table row — the handle path carries no type arguments. That
generic-type machinery is the generics epoch's job; the atomics epoch that
follows binds the cell on top of it.

## The two lock-free patterns

**CAS retry loop (Treiber stack, ring buffer).**

```aril
// push: build the new node pointing at the current head, CAS it in, retry on race.
func push(head: atomic.Pointer<Node<T>>, v: T) {
  while true {
    let old = head.load()
    let node = Node<T>{ value: v, next: old }
    if head.compareAndSwap(old, Some(node)) { return }
  }
}
```

**RCU (ordered set — skip-list / tree).** Readers never block: they `load()` the
root and walk an immutable snapshot. A writer copies the affected path, links the
copy to the unchanged remainder, and `compareAndSwap`es the root to publish. The
GC reclaims the superseded path once the last reader releases it.

## Contract discipline

Lock-free code is exactly where a wrong invariant hides, so every lock-free
example carries contracts that **fail on a broken implementation** — not
`x == x` trivia:

- Treiber stack — the multiset of popped elements equals the multiset pushed (no
  lost / duplicated node under contention), LIFO per producer;
- ring buffer — never more than `cap` in flight, no overwrite of an unconsumed
  slot, FIFO per producer;
- RCU set — membership after a concurrent insert/delete batch matches a
  sequential oracle; readers observe a consistent snapshot (no torn structure).

The stress harness uses the multi-input `[[case]]` template (many
producers/consumers, several element counts) with oracle-derived expected output.

## Staging

1. **Aspirational examples (`target-sketch`).** Author the containers against this
   surface. They do not build (the surface is unbound and generic classes have
   open monomorphization gaps); they are the demand signal and acceptance target.
   Each must fail *gracefully* (a diagnostic or a `go build` miss, never a
   compiler panic).
2. **Generics work.** Deliver the generic-type machinery `atomic.Pointer<T>`
   needs, plus the generic-class fixes the container classes require
   (implicit-receiver resolution, `Option`/`None` instantiation in generic
   bodies, `comparable` propagation).
3. **Atomics binding.** Bind the scalar atomics (a table edit) and
   `atomic.Pointer<T>` (the generic atomic cell). Flip the aspirational examples
   to build and run, `-race`-clean.

## Rejected alternatives

- **`atomic.Value` (holds `any`) as the RCU primitive.** Non-generic, needs no
  generic-type machinery — but untyped (`load()` returns `Dynamic`, every use a
  cast) and dishonest as the primary cell. `atomic.Pointer<T>` is the typed
  answer, deferred behind generics rather than mismodelled.
- **Raw pointers + manual reclamation.** Aril has no `unsafe`, and the GC makes
  manual reclamation unnecessary and, in fact, wrong.
