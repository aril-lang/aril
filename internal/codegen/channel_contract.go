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
// Binding is by NAME at the use site (an Ident whose name is a contracted
// subject) over a bidirectional `Channel` receiver — the creator/owner frame
// where the channel is `chan T`. A directional `SendChan`/`RecvChan` (typically
// a callee parameter) is left unmonitored; cross-function enforcement is a
// follow-up. Capacity (E1206), closed-by (E1201), recv-after-close (E1204) and
// the cross-channel trace kinds are recognized but not yet enforced.

// detectChannelContracts sets usesChanContract when an enforced channel trace
// contract is present (panic mode) — so the arilrt import / inline prelude is
// emitted by writeHeader, which runs before the bodies that also set the flag.
func (g *gen) detectChannelContracts() {
	if g.contractMode != "panic" || g.info == nil {
		return
	}
	for name := range g.info.ChannelContracts {
		if g.chanEnforced(name) {
			g.usesChanContract = true
			return
		}
	}
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

// isBidiChannel reports whether the Ident is a bidirectional channel binding
// (`Channel`, not a directional `SendChan`/`RecvChan`) — the monitored helpers
// take `chan T`, so only the bidirectional creator/owner frame is wrapped.
func (g *gen) isBidiChannel(id *ast.Ident) bool {
	return g.varKindOf(id) == "Channel"
}

// emitChannelContractReg registers a contracted channel at its creation site
// (after the binding line) and, for a `drains-before-…` subject, schedules the
// boundary drain check with a `defer`. The binding value must be a
// bidirectional channel.
func (g *gen) emitChannelContractReg(name string, value ast.Expr, span ast.Span) {
	if !g.chanEnforced(name) {
		return
	}
	if _, ok := g.info.Type[value].(*sema.Channel); !ok {
		return
	}
	g.usesChanContract = true
	g.writeIndent()
	g.b.WriteString(g.rt("RegisterChan") + "(" + goIdent(name) + ", " + strconv.Quote(name) + ")\n")
	if chanHasKind(g.chanClauses(name), "drains-before-scope-exit", "drains-before-return") {
		g.writeIndent()
		g.b.WriteString("defer " + g.rt("ChanCheckDrained") + "(" + goIdent(name) + ", " + strconv.Quote(g.srcLoc(span)) + ")\n")
	}
}

// chanSendChecked reports whether a `.send` on this receiver must route through
// the monitored helper (a `forbid send after close` subject, bidirectional).
func (g *gen) chanSendChecked(id *ast.Ident) bool {
	return g.isBidiChannel(id) && chanHasKind(g.chanClauses(id.Name), "forbid-send-after-close")
}

// chanCloseChecked reports whether a `.close` on this receiver must route
// through the monitored helper (any enforced subject — for double-close
// detection and to record the close for the send / drain checks).
func (g *gen) chanCloseChecked(id *ast.Ident) bool {
	return g.isBidiChannel(id) && g.chanEnforced(id.Name)
}

// srcLoc renders an `.aril` source location for a violation message (D10).
func (g *gen) srcLoc(span ast.Span) string {
	return fmt.Sprintf("%s:%d:%d", g.file, span.StartLine, span.StartCol)
}
