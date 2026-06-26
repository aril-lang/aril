package codegen

import (
	"fmt"
	"strconv"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/sema"
)

// Channel contract lowering (RFC-0007). Mirrors the sema side
// (sema/channel_contract.go): the resolved local channel clauses live on
// Info.ChannelContracts, keyed by subject name. v1 enforces the definitive
// local subset — close-safety (`forbid send after close` → E1203, double close
// → E1202) and drain-at-boundary (`drains-before-…` → E1207) — by installing
// an arilrt monitor at the channel's creation site (RegisterChan) and routing
// its `.send` / `.close` through the monitored helpers. Everything is gated on
// `--contracts=panic`; under off nothing is emitted (byte-identical lowering).
//
// Registration is by NAME at the channel's creation site (a bidirectional
// `Channel` value), but RegisterChan keys every directional view, so a `.send` /
// `.close` from a callee frame that received the channel as a directional
// `SendChan` parameter still finds the monitor — the cross-function enforcement.
// A named bidi receiver routes precisely by its own clauses; a directional
// `SendChan` receiver (source name lost across the boundary) routes whenever the
// program carries any relevant contract, and the runtime no-ops an unregistered
// channel. Bidi aliasing is the one remaining unmonitored path (a follow-up).
// Capacity (E1206), closed-by (E1201), recv-after-close (E1204) and the
// cross-channel trace kinds are recognized but not yet enforced.

// detectChannelContracts sets usesChanContract when an enforced channel trace
// contract is present (panic mode) — so the arilrt import / inline prelude is
// emitted by writeHeader, which runs before the bodies that also set the flag.
func (g *gen) detectChannelContracts() {
	if g.anyChanEnforced() {
		g.usesChanContract = true
	}
}

// anyChanEnforced reports whether the program carries any channel contract v1
// enforces at runtime (panic mode) — the gate for routing directional channel
// closes through the monitor (a directional callee frame has lost the source
// subject name, so it can't decide per-channel; the runtime no-ops an
// unregistered channel).
func (g *gen) anyChanEnforced() bool {
	if g.contractMode != "panic" || g.info == nil {
		return false
	}
	for name := range g.info.ChannelContracts {
		if g.chanEnforced(name) {
			return true
		}
	}
	return false
}

// anyChanForbidSend reports whether any channel subject carries `forbid send
// after close` — the gate for routing directional sends (same source-name-lost
// reasoning as anyChanEnforced).
func (g *gen) anyChanForbidSend() bool {
	if g.contractMode != "panic" || g.info == nil {
		return false
	}
	for name := range g.info.ChannelContracts {
		if chanHasKind(g.chanClauses(name), "forbid-send-after-close") {
			return true
		}
	}
	return false
}

// chanClauses returns a contracted channel subject's local clauses under panic
// mode, or nil (off elides everything / not contracted).
func (g *gen) chanClauses(name string) []ast.ChannelClause {
	if g.contractMode != "panic" || g.info == nil {
		return nil
	}
	if cc := g.info.ChannelContracts[name]; cc != nil {
		return cc.Clauses
	}
	return nil
}

func chanHasKind(clauses []ast.ChannelClause, kinds ...string) bool {
	for _, cl := range clauses {
		for _, k := range kinds {
			if cl.Kind == k {
				return true
			}
		}
	}
	return false
}

// chanEnforced reports whether the named subject carries a clause v1 enforces
// at runtime (`forbid send after close` or a `drains-before-…`), so its
// channel must be registered and its close routed through the monitor.
func (g *gen) chanEnforced(name string) bool {
	cl := g.chanClauses(name)
	return chanHasKind(cl, "forbid-send-after-close", "drains-before-scope-exit", "drains-before-return")
}

// emitChannelContractReg registers a contracted channel at its creation site
// (after the binding line) and, for a `drains-before-…` subject, schedules the
// boundary drain check with a `defer`. The binding value must be a
// bidirectional channel (the creator/owner frame). RegisterChan keys all
// directional views, so a later directional send/close still finds the state.
func (g *gen) emitChannelContractReg(name string, value ast.Expr, span ast.Span) {
	if !g.chanEnforced(name) {
		return
	}
	if _, ok := g.info.Type[value].(*sema.Channel); !ok {
		return
	}
	g.usesChanContract = true
	forbidSend := chanHasKind(g.chanClauses(name), "forbid-send-after-close")
	g.writeIndent()
	g.b.WriteString(g.rt("RegisterChan") + "(" + goIdent(name) + ", " + strconv.Quote(name) + ", " + strconv.FormatBool(forbidSend) + ")\n")
	if chanHasKind(g.chanClauses(name), "drains-before-scope-exit", "drains-before-return") {
		g.writeIndent()
		g.b.WriteString("defer " + g.rt("ChanCheckDrained") + "(" + goIdent(name) + ", " + strconv.Quote(g.srcLoc(span)) + ")\n")
	}
}

// chanSendChecked reports whether a `.send` on this receiver routes through the
// monitored helper. A named bidirectional creator/owner frame (`Channel`) is
// matched precisely by its `forbid send after close` clause — byte-identical to
// the pre-directional lowering. A directional `SendChan` view has lost the
// source subject name across the function boundary, so it routes whenever the
// program has any `forbid send after close` contract; the runtime monitor
// no-ops an unregistered channel.
func (g *gen) chanSendChecked(id *ast.Ident) bool {
	switch g.varKindOf(id) {
	case "Channel":
		return chanHasKind(g.chanClauses(id.Name), "forbid-send-after-close")
	case "SendChan":
		return g.anyChanForbidSend()
	}
	return false
}

// chanCloseChecked reports whether a `.close` on this receiver routes through
// the monitored helper (double-close E1202 + recording the close for the send /
// drain checks). Named bidirectional frame: precise per-subject match.
// Directional `SendChan` view: routes whenever any subject is enforced (the
// runtime filters), since a `chan<- T` is closable but its name is lost.
func (g *gen) chanCloseChecked(id *ast.Ident) bool {
	switch g.varKindOf(id) {
	case "Channel":
		return g.chanEnforced(id.Name)
	case "SendChan":
		return g.anyChanEnforced()
	}
	return false
}

// srcLoc renders an `.aril` source location for a violation message (D10).
func (g *gen) srcLoc(span ast.Span) string {
	return fmt.Sprintf("%s:%d:%d", g.file, span.StartLine, span.StartCol)
}
