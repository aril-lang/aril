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
		"RegisterChan(ch, \"ch\", true)",
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

// TestEmitChannelContractDirectional: a contracted channel created in one frame
// and passed to a directional `SendChan` callee has its directional `.send`
// routed through the monitor. Registration at the creation site keys every
// directional view, so the callee's `chan<- T` send finds the shared state —
// the cross-function enforcement. The contract is declared on the creation-site
// subject (`ch`), the only place registration can fire.
func TestEmitChannelContractDirectional(t *testing.T) {
	src := `func feed(out: SendChan<int>) {
  out.send(1)
}

func main() {
  let ch = makeChannel<int>(2)
  feed(ch)
  ch.close()
}

channel ch {
  forbid send after close
}
`
	panicMode := emitContract(t, src, "panic")
	for _, want := range []string{
		"RegisterChan(ch, \"ch\", true)", // registered at the creation site
		"ChanSend(out, 1,",               // directional callee send routed
		"ChanClose(ch,",                  // bidi close routed (name-matched)
	} {
		if !strings.Contains(panicMode, want) {
			t.Errorf("panic-mode emit missing %q:\n%s", want, panicMode)
		}
	}

	// off elides everything (byte-identical lowering).
	off := emitContract(t, src, "off")
	for _, bad := range []string{"RegisterChan", "ChanSend", "ChanClose"} {
		if strings.Contains(off, bad) {
			t.Errorf("off-mode emit must not route channel ops (%q):\n%s", bad, off)
		}
	}
}
