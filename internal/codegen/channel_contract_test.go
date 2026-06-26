package codegen

import (
	"strings"
	"testing"
)

const chanContractSrc = `func main() {
  let ch = makeChannel<int>(2)
  ch.send(1)
  ch.close()
}

channel ch {
  forbid send after close
  drains-before-scope-exit
}
`

// TestEmitChannelContract: under --contracts=panic an enforced channel contract
// installs the arilrt monitor — RegisterChan at the creation site, a deferred
// ChanCheckDrained for a drains subject, and routes `.send` / `.close` through
// ChanSend / ChanClose. Under off nothing is emitted (byte-identical lowering).
func TestEmitChannelContract(t *testing.T) {
	panicMode := emitContract(t, chanContractSrc, "panic")
	for _, want := range []string{
		"RegisterChan(ch, \"ch\")",
		"ChanCheckDrained(ch,",
		"ChanSend(ch, 1,",
		"ChanClose(ch,",
	} {
		if !strings.Contains(panicMode, want) {
			t.Errorf("panic-mode emit missing %q:\n%s", want, panicMode)
		}
	}

	off := emitContract(t, chanContractSrc, "off")
	for _, bad := range []string{"RegisterChan", "ChanSend", "ChanClose", "ChanCheckDrained"} {
		if strings.Contains(off, bad) {
			t.Errorf("off-mode emit must not contain %q (byte-identical lowering):\n%s", bad, off)
		}
	}
	// off mode is byte-identical to the same program without the contract.
	noContract := emitContract(t, `func main() {
  let ch = makeChannel<int>(2)
  ch.send(1)
  ch.close()
}
`, "off")
	if off != noContract {
		t.Errorf("off-mode emit is not byte-identical to the contract-free program:\noff:\n%s\nno-contract:\n%s", off, noContract)
	}
}

// TestEmitChannelContractDirectionalSkipped: a directional SendChan parameter is
// NOT routed through the monitor (the helpers take a bidirectional `chan T`);
// cross-function enforcement is a follow-up.
func TestEmitChannelContractDirectionalSkipped(t *testing.T) {
	src := `func feed(out: SendChan<int>) {
  out.send(1)
}

func main() {
  let ch = makeChannel<int>(2)
  feed(ch)
  ch.close()
}

channel out {
  forbid send after close
}
`
	panicMode := emitContract(t, src, "panic")
	// The directional `out.send` in feed stays a bare channel send.
	if strings.Contains(panicMode, "ChanSend(out") {
		t.Errorf("a directional SendChan send must not route through the monitor:\n%s", panicMode)
	}
}
