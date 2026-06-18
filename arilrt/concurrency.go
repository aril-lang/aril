package arilrt

import (
	"context"
	"sync"
)

// concurrency.go — TryRecv (non-blocking channel receive → Option) and
// the structured-concurrency Group backing scope / spawn (lowering-go.md
// §ScopeIR / §SpawnIR). Group replicates errgroup.WithContext semantics
// with only sync + context (generated modules carry no external deps):
// the first spawned func to return a non-nil error stores it and cancels
// the derived context; Wait blocks for all spawns and returns that error.

// TryRecv returns Some(v) when a value is ready on ch, None otherwise.
func TryRecv[T any](ch <-chan T) Option[T] {
	select {
	case v := <-ch:
		return OptionSome[T](v)
	default:
		return OptionNone[T]()
	}
}

// Group is a structured-concurrency scope.
type Group struct {
	wg     sync.WaitGroup
	once   sync.Once
	err    error
	cancel context.CancelFunc
}

// NewGroup derives a cancellable context and a Group bound to it.
func NewGroup(parent context.Context) (*Group, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	return &Group{cancel: cancel}, ctx
}

// Go spawns f; the first non-nil error cancels the group's context.
func (g *Group) Go(f func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := f(); err != nil {
			g.once.Do(func() {
				g.err = err
				g.cancel()
			})
		}
	}()
}

// Wait blocks for all spawns and returns the first error (if any).
func (g *Group) Wait() error {
	g.wg.Wait()
	g.cancel()
	return g.err
}
