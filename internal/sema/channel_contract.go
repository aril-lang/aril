package sema

import "github.com/aril-lang/aril/internal/ast"

// Channel contract checking (RFC-0007 trace contracts). Two surfaces:
//   - separable `channel <subject> { … }` blocks in File.Channels — local trace
//     invariants of one channel (closed-by / forbid send|recv after close /
//     capacity / drains-before-…);
//   - cross-channel *protocol* clauses on a `contract` block in File.Contracts
//     whose Target is a logical protocol name (not a value/state target):
//     subject decls, ordering/liveness, fan-out, fairness.
//
// Binding model (v1): a contract subject names a channel-typed value of the
// same name — the same name-based dispatch codegen already uses for channel
// method calls. A subject naming no channel value is unbound → E1210. Anonymous
// channel expressions are unnameable in a clause by construction.
//
// This pass runs after body checking, so each subject can be matched against
// the inferred types of the program's bindings. It does well-formedness +
// subject binding + IR (Info.ChannelContracts, the local clauses codegen
// enforces). The cross-channel protocol clauses are validated for
// well-formedness but their runtime enforcement (a global trace monitor) is a
// documented follow-up — they are recognized, not yet enforced.

// protocolClauseKinds are the RFC-0007 ContractClause kinds. A `contract` block
// carrying any of these is a protocol contract — exempt from the RFC-0006
// value-contract target resolution (E1101), since its Target is a logical name.
var protocolClauseKinds = map[string]bool{
	"channel-subject": true, "participant": true,
	"forbid-before": true, "eventually-after": true, "every-eventually": true,
	"delivered-to-all": true, "fairness": true,
}

// subjectRoles is the closed RFC-0007 role-label set (diagnostics-only labels).
var subjectRoles = map[string]bool{"cancel": true, "timeout": true, "signal": true}

func hasProtocolClause(cd *ast.ContractDecl) bool {
	for _, cl := range cd.Clauses {
		if protocolClauseKinds[cl.Kind] {
			return true
		}
	}
	return false
}

// hasValueClause reports whether cd carries any RFC-0006 value/state clause
// (requires/ensures/invariant/loop/entry) — anything not a protocol clause.
func hasValueClause(cd *ast.ContractDecl) bool {
	for _, cl := range cd.Clauses {
		if !protocolClauseKinds[cl.Kind] {
			return true
		}
	}
	return false
}

func isChannelType(t Type) bool {
	switch t.(type) {
	case *Channel, *SendChan, *RecvChan:
		return true
	}
	return false
}

// checkChannelContracts validates the channel-block and protocol-contract
// surfaces and records the local channel clauses on Info.ChannelContracts.
// Called from CheckFiles after body checking.
func (c *checker) checkChannelContracts(files []*ast.File, paths []string) {
	chanNames := c.channelBindingNames()
	for i, f := range files {
		c.file = paths[i]
		for _, cd := range f.Channels {
			c.checkChannelDecl(cd, chanNames)
		}
	}
	for _, pc := range c.protocolContracts {
		c.file = pc.file
		c.checkProtocolContract(pc.decl, chanNames)
	}
}

// channelBindingNames collects the names of every channel-typed binding in the
// program (let / param / field, via the Def back-reference), the candidate
// subjects a channel contract may name.
func (c *checker) channelBindingNames() map[string]bool {
	names := map[string]bool{}
	for _, sym := range c.info.Def {
		if isChannelType(sym.Type) {
			names[sym.Name] = true
		}
	}
	return names
}

// checkChannelDecl binds a `channel <subject> { … }` block to its channel value
// and records the local clauses for codegen.
func (c *checker) checkChannelDecl(cd *ast.ChannelDecl, chanNames map[string]bool) {
	if !chanNames[cd.Subject] {
		c.report("E1210",
			"channel contract names subject `"+cd.Subject+"`, but no channel value of that name exists",
			cd.Span)
		return
	}
	cc := c.info.ChannelContracts[cd.Subject]
	if cc == nil {
		cc = &ChannelContract{Subject: cd.Subject}
		c.info.ChannelContracts[cd.Subject] = cc
	}
	cc.Clauses = append(cc.Clauses, cd.Clauses...)
}

// checkProtocolContract validates a cross-channel protocol contract's
// well-formedness: subjects/participants are declared, roles are in the closed
// set, events have the `subject.op(payload)` shape over a declared subject, and
// fan-out / fairness targets reference declared names. All ill-formedness is
// E1210.
func (c *checker) checkProtocolContract(cd *ast.ContractDecl, chanNames map[string]bool) {
	subjects := map[string]bool{}
	participants := map[string]bool{}
	for _, cl := range cd.Clauses {
		switch cl.Kind {
		case "channel-subject":
			subjects[cl.Subject] = true
			if cl.Role != "" && !subjectRoles[cl.Role] {
				c.report("E1210", "unknown subject role `"+cl.Role+"` (expected cancel/timeout/signal)", cl.Span)
			}
			if !chanNames[cl.Subject] {
				c.report("E1210",
					"protocol contract declares channel subject `"+cl.Subject+"`, but no channel value of that name exists",
					cl.Span)
			}
		case "participant":
			participants[cl.Subject] = true
		}
	}
	for _, cl := range cd.Clauses {
		switch cl.Kind {
		case "forbid-before", "eventually-after", "every-eventually":
			c.checkEvent(cl.EventA, subjects, cl.Span)
			c.checkEvent(cl.EventB, subjects, cl.Span)
		case "delivered-to-all":
			if !subjects[cl.Subject] {
				c.report("E1210", "fan-out source `"+cl.Subject+"` is not a declared channel subject", cl.Span)
			}
			if cl.RecvSet != "" && !participants[cl.RecvSet] {
				c.report("E1210", "fan-out receiver set `"+cl.RecvSet+"` is not a declared participant", cl.Span)
			}
			for _, m := range cl.Names {
				if !participants[m] {
					c.report("E1210", "fan-out member `"+m+"` is not a declared participant", cl.Span)
				}
			}
		case "fairness":
			for _, s := range cl.Names {
				if !subjects[s] && !participants[s] {
					c.report("E1210", "no-starvation subject `"+s+"` is not a declared subject or participant", cl.Span)
				}
			}
		}
	}
}

// checkEvent validates one protocol event Expr: it must have the shape
// `subject.op(payload)` (or `subject.close`), with op ∈ send/recv/close and the
// subject a declared channel subject of the contract.
func (c *checker) checkEvent(e ast.Expr, subjects map[string]bool, span ast.Span) {
	if e == nil {
		return
	}
	subj, op, isCall, ok := eventShape(e)
	if !ok {
		c.report("E1210", "a protocol event must have the form `subject.op(payload)` with op ∈ send/recv/close", span)
		return
	}
	if op != "send" && op != "recv" && op != "close" {
		c.report("E1210", "unknown event operation `"+op+"` (expected send/recv/close)", span)
		return
	}
	// `send`/`recv` carry a payload (`subject.send(p)` / `subject.recv(_)`);
	// `close` is the bare `subject.close` form (RFC-0007 §Design).
	if op == "close" && isCall {
		c.report("E1210", "a `close` event takes no payload — write `"+subj+".close`", span)
		return
	}
	if op != "close" && !isCall {
		c.report("E1210", "a `"+op+"` event needs a payload — write `"+subj+"."+op+"(payload)`", span)
		return
	}
	if !subjects[subj] {
		c.report("E1210", "event subject `"+subj+"` is not a declared channel subject", span)
	}
}

// eventShape extracts (subject, op, isCall) from a `subject.op(payload)` or
// `subject.close` event Expr — a Field over an Ident receiver, optionally
// wrapped in a Call (the payload args). isCall reports whether the form carried
// a call (a payload). Returns ok == false for any other shape.
func eventShape(e ast.Expr) (subj, op string, isCall, ok bool) {
	if call, ok := e.(*ast.Call); ok {
		e = call.Callee
		isCall = true
	}
	fld, isFld := e.(*ast.Field)
	if !isFld {
		return "", "", isCall, false
	}
	id, isID := fld.Receiver.(*ast.Ident)
	if !isID {
		return "", "", isCall, false
	}
	return id.Name, fld.Name, isCall, true
}
