package arilrt

// atomics.go — AtomicPointer, the honest generic atomic reference cell backing
// Aril's atomic.Pointer<T> (docs/atomics-lock-free.md §Atomic references).
//
// Aril has no raw pointers: a class instance is a Go *T and "nil" is None, so
// the cell's load/swap traffic in Option[*T] (Some = a live reference, None =
// the nil pointer). CAS compares by Go pointer identity — RCU swaps the
// *identity* of the published version, not its structure. The GC is the
// reclamation grace period (a superseded version stays reachable exactly as
// long as a reader still holds it), so this needs nothing from unsafe/runtime.

import "sync/atomic"

// AtomicPointer wraps Go's atomic.Pointer[T], which stores a *T. T is the
// pointee (the class struct), so a load yields *T lifted into Option[*T].
type AtomicPointer[T any] struct {
	p atomic.Pointer[T]
}

// Load returns the current reference (lock-free), None when the cell is empty.
func (a *AtomicPointer[T]) Load() Option[*T] {
	return optOfPtr(a.p.Load())
}

// Store publishes a reference. The Aril method takes a bare T (a class value,
// which is a Go *T); a cell is emptied via Swap(None), never Store.
func (a *AtomicPointer[T]) Store(v *T) { a.p.Store(v) }

// Swap publishes new and returns the previous reference (as Option).
func (a *AtomicPointer[T]) Swap(v Option[*T]) Option[*T] {
	return optOfPtr(a.p.Swap(ptrOfOpt(v)))
}

// CompareAndSwap publishes new iff the cell still holds old (by pointer
// identity), returning whether it won the race.
func (a *AtomicPointer[T]) CompareAndSwap(old, new Option[*T]) bool {
	return a.p.CompareAndSwap(ptrOfOpt(old), ptrOfOpt(new))
}

// optOfPtr lifts a raw *T into Option[*T] (nil → None).
func optOfPtr[T any](v *T) Option[*T] {
	if v != nil {
		return OptionSome[*T](v)
	}
	return OptionNone[*T]()
}

// ptrOfOpt lowers Option[*T] back to a raw *T (None → nil).
func ptrOfOpt[T any](o Option[*T]) *T {
	if o.Tag == 1 {
		return o.V
	}
	return nil
}
