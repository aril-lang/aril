package arilrt

import "testing"

// mustPanic runs f and asserts it panicked with a message containing want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q, got none", want)
		}
		if msg, ok := r.(string); !ok || !contains(msg, want) {
			t.Fatalf("panic = %v, want it to contain %q", r, want)
		}
	}()
	f()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestChanContractCleanPath: a registered channel that is sent-to then closed,
// then drain-checked, raises nothing.
func TestChanContractCleanPath(t *testing.T) {
	ch := make(chan int, 1)
	RegisterChan(ch, "ok")
	ChanSend(ch, 1, "loc")
	<-ch
	ChanClose(ch, "loc")
	ChanCheckDrained(ch, "loc")
}

// TestChanContractSendAfterClose: a send on a closed contracted channel is
// E1203, raised by the monitor before the (panicking) real send.
func TestChanContractSendAfterClose(t *testing.T) {
	ch := make(chan int, 1)
	RegisterChan(ch, "sac")
	ChanClose(ch, "loc")
	mustPanic(t, "[E1203]", func() { ChanSend(ch, 1, "loc") })
}

// TestChanContractDoubleClose: closing a contracted channel twice is E1202.
func TestChanContractDoubleClose(t *testing.T) {
	ch := make(chan int, 1)
	RegisterChan(ch, "dc")
	ChanClose(ch, "loc")
	mustPanic(t, "[E1202]", func() { ChanClose(ch, "loc") })
}

// TestChanContractDrainLeak: a contracted channel not closed before its
// boundary is E1207.
func TestChanContractDrainLeak(t *testing.T) {
	ch := make(chan int, 1)
	RegisterChan(ch, "leak")
	mustPanic(t, "[E1207]", func() { ChanCheckDrained(ch, "loc") })
}

// TestChanContractUnregistered: monitor calls on an unregistered channel are
// no-ops (only contracted channels pay any cost).
func TestChanContractUnregistered(t *testing.T) {
	ch := make(chan int, 1)
	ChanSend(ch, 1, "loc") // no panic — not registered
	<-ch
	ChanClose(ch, "loc")        // no double-close tracking
	ChanCheckDrained(ch, "loc") // no drain check
}
