package arilrt

import (
	"fmt"
	"sync"
)

// Channel trace-contract monitor (RFC-0007). v1 enforces the definitive local
// close-safety subset (double-close E1202, send-after-close E1203) and the
// drain-at-boundary completion check (E1207), keyed by the channel VALUE — so a
// registered channel is monitored wherever the value flows, across goroutines.
// Operations on an unregistered channel are no-ops, and codegen emits these
// calls only under `--contracts=panic` (under off nothing is emitted →
// byte-identical lowering). The monitor is goroutine-safe: channels cross
// `spawn`/`scope` boundaries, so each subject's state is mutex-guarded.
//
// Directionality: a contracted channel is created bidirectional (`chan T`) but
// flows into callees as a directional `chan<- T` / `<-chan T` parameter, which
// boxes to a *different* dynamic type — so a single `any`-key registration would
// miss the directional view. RegisterChan therefore stores all three views,
// sharing one state, so a send/close from any frame finds the monitor. The
// `forbidSend` flag carries whether send-after-close is an E1203 violation for
// this subject: codegen routes *every* directional send through ChanSend (the
// source name is lost across the boundary, so it can't decide per-channel), and
// the flag keeps a non-`forbid send after close` channel a no-op.
//
// The companion inline forms (predeclared.go writePredeclaredChanContract) must
// stay byte-equivalent in behaviour (the runtime-mode equivalence guard).

type chanContractState struct {
	mu         sync.Mutex
	name       string
	forbidSend bool
	closed     bool
}

// chanContracts maps a contracted channel value (each directional view) to its
// shared monitor state.
var chanContracts sync.Map // map[any]*chanContractState

// RegisterChan records a contracted channel under its source subject name,
// keyed by every directional view of the channel so a send/close from a
// directional callee frame still finds the state. forbidSend records whether
// `forbid send after close` (E1203) applies. Idempotent — re-registering keeps
// the first state.
func RegisterChan[T any](ch chan T, name string, forbidSend bool) {
	s := &chanContractState{name: name, forbidSend: forbidSend}
	chanContracts.LoadOrStore(ch, s)
	chanContracts.LoadOrStore((chan<- T)(ch), s)
	chanContracts.LoadOrStore((<-chan T)(ch), s)
}

func chanContractOf(ch any) *chanContractState {
	if v, ok := chanContracts.Load(ch); ok {
		return v.(*chanContractState)
	}
	return nil
}

// ChanSend sends v on ch, first asserting the channel is not closed
// (`forbid send after close`, E1203) when ch is a contracted subject that
// forbids it. Takes a send-directional channel so both a bidirectional creator
// frame and a `chan<- T` callee frame route through it.
func ChanSend[T any](ch chan<- T, v T, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		bad, name := s.forbidSend && s.closed, s.name
		s.mu.Unlock()
		if bad {
			panic(chanViolation("E1203", name, "send after close", loc))
		}
	}
	ch <- v
}

// ChanClose closes ch, asserting it was not already closed (double close,
// E1202) and recording the close so later sends / the drain check can see it.
func ChanClose[T any](ch chan<- T, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		if s.closed {
			name := s.name
			s.mu.Unlock()
			panic(chanViolation("E1202", name, "double close", loc))
		}
		s.closed = true
		s.mu.Unlock()
	}
	close(ch)
}

// ChanCheckDrained asserts a contracted channel was closed before its owning
// boundary (scope exit / function return) — `drains-before-scope-exit` /
// `drains-before-return`, E1207. v1 checks closed (a leaked, never-closed
// channel); the stricter "closed AND empty" needs in-flight accounting (a
// follow-up). Emitted as a `defer` at the channel's creation site.
func ChanCheckDrained(ch any, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		closed, name := s.closed, s.name
		s.mu.Unlock()
		if !closed {
			panic(chanViolation("E1207", name, "not closed before its owning boundary", loc))
		}
	}
}

func chanViolation(code, name, what, loc string) string {
	return fmt.Sprintf("aril: channel contract violated [%s] at %s: channel `%s` — %s", code, loc, name, what)
}
