package sema

import "testing"

// TestChannelContractBound: a `channel <subject> { … }` block naming a real
// channel local binds and records its clauses on Info.ChannelContracts with no
// diagnostics.
func TestChannelContractBound(t *testing.T) {
	info, codes := checkContract(t, `func run() {
  let results = makeChannel<int>(4)
  results.send(1)
  results.close()
}

channel results {
  closed-by run
  forbid send after close
  never more than 4 in flight
  drains-before-scope-exit
}
`)
	if len(codes) != 0 {
		t.Fatalf("clean channel contract produced diags: %v", codes)
	}
	cc := info.ChannelContracts["results"]
	if cc == nil {
		t.Fatalf("expected ChannelContracts[results] to be recorded")
	}
	if len(cc.Clauses) != 4 {
		t.Errorf("expected 4 channel clauses recorded, got %d", len(cc.Clauses))
	}
}

// TestChannelContractUnbound: a channel block naming no channel value is E1210.
func TestChannelContractUnbound(t *testing.T) {
	_, codes := checkContract(t, `func run() { return }

channel ghost {
  closed-by run
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for an unbound channel subject, got %v", codes)
	}
}

// TestProtocolContractNotE1101: a protocol contract attaches to channels by
// subject, not a value/state target — it must NOT trip the value-contract
// E1101 "no such declaration".
func TestProtocolContractNotE1101(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let work = makeChannel<int>(1)
  let results = makeChannel<int>(1)
}

contract WorkerPool {
  channel work
  channel results
  forbid results.send(x) before work.recv(y)
  eventually results.close after work.close
  fairness { no-starvation work }
}
`)
	if containsCode(codes, "E1101") {
		t.Fatalf("protocol contract wrongly produced E1101: %v", codes)
	}
	if len(codes) != 0 {
		t.Fatalf("clean protocol contract produced diags: %v", codes)
	}
}

// TestProtocolEventUndeclaredSubject: an event over a subject the contract
// never declared is E1210.
func TestProtocolEventUndeclaredSubject(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let work = makeChannel<int>(1)
}

contract C {
  channel work
  forbid bogus.send(x) before work.recv(y)
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for an undeclared event subject, got %v", codes)
	}
}

// TestProtocolBadRole: a subject role outside cancel/timeout/signal is E1210.
func TestProtocolBadRole(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let done = makeChannel<int>(1)
}

contract C {
  channel done role bogus
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for an unknown subject role, got %v", codes)
	}
}

// TestProtocolFanoutBadMember: a delivered-to-all member that is not a declared
// participant is E1210.
func TestProtocolFanoutBadMember(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let deadline = makeChannel<int>(1)
}

contract C {
  channel deadline
  participant producer
  deadline delivered-to-all { producer, ghost }
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for an undeclared fan-out member, got %v", codes)
	}
}

// TestProtocolMixedBlock: a contract mixing value/state clauses with protocol
// clauses is E1210 (split them) — the value clauses would otherwise be dropped.
func TestProtocolMixedBlock(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let work = makeChannel<int>(1)
}

contract C {
  requires true
  channel work
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for a mixed value/protocol contract, got %v", codes)
	}
}

// TestProtocolBareSendRejected: a `send`/`recv` event without a payload is
// E1210 (close is the only bare event).
func TestProtocolBareSendRejected(t *testing.T) {
	_, codes := checkContract(t, `func run() {
  let work = makeChannel<int>(1)
  let results = makeChannel<int>(1)
}

contract C {
  channel work
  channel results
  forbid results.send before work.recv(y)
}
`)
	if !containsCode(codes, "E1210") {
		t.Fatalf("expected E1210 for a payload-less send event, got %v", codes)
	}
}

func containsCode(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}
