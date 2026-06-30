package codegen

import (
	"strconv"

	"github.com/aril-lang/aril/internal/ast"
)

// Labeled-break lowering (§LabeledBreak, lowering-go.md).
//
// An Aril `break` always targets its nearest enclosing loop. But a
// `match` lowers to a Go `switch` and a `select` to a Go `select`, both
// of which CAPTURE a bare `break` — so a `break` written inside a
// `match` arm (or `select` case) within a loop would break the switch,
// not the loop, silently turning the loop infinite. When a loop body
// contains such a break, the loop is emitted with a Go label and those
// breaks lower to `break <label>`.
//
// `continue` needs no such treatment: Go's `switch`/`select` do not
// capture `continue`, so a bare `continue` already targets the loop.

// emitLoop brackets the emission of one loop (for/while) with its loop
// frame: it assigns and emits a Go label first when a `break` in the
// body would otherwise be captured by a lowered switch/select, then runs
// `emit` (which writes the `for`) with the frame on the stack. This is
// the single place the loop-frame lifecycle lives, so the individual
// loop emitters stay pure emission — it is invoked once from the
// statement dispatch for every loop node.
func (g *gen) emitLoop(body *ast.Block, emit func() error) error {
	if loopBodyBreakNeedsLabel(body) {
		g.loopLabelCounter++
		g.loopFrames = append(g.loopFrames, &loopFrame{label: goLoopLabel(g.loopLabelCounter)})
		g.writeIndent()
		g.b.WriteString(g.curLoop().label)
		g.b.WriteString(":\n")
	} else {
		g.loopFrames = append(g.loopFrames, &loopFrame{})
	}
	defer func() { g.loopFrames = g.loopFrames[:len(g.loopFrames)-1] }()
	return emit()
}

// curLoop returns the innermost enclosing loop frame, or nil at top
// level / inside a closure that opened no loop.
func (g *gen) curLoop() *loopFrame {
	if len(g.loopFrames) == 0 {
		return nil
	}
	return g.loopFrames[len(g.loopFrames)-1]
}

// enterSwitch marks that a match/select switch frame has opened inside
// the innermost loop; exitSwitch (deferred by the caller) closes it.
// Returns the frame whose depth was bumped so the matching decrement
// hits the same frame even if a nested loop pushes in between.
func (g *gen) enterSwitch() *loopFrame {
	f := g.curLoop()
	if f != nil {
		f.switchDepth++
	}
	return f
}

func (g *gen) exitSwitch(f *loopFrame) {
	if f != nil {
		f.switchDepth--
	}
}

// breakTarget returns the labelled-break keyword to emit for a `break`
// at the current point: `break <label>` when a switch/select would
// otherwise capture it, else a plain `break`.
func (g *gen) breakTarget() string {
	f := g.curLoop()
	if f != nil && f.switchDepth > 0 && f.label != "" {
		return "break " + f.label
	}
	return "break"
}

func goLoopLabel(n int) string {
	return "_arilLoop" + strconv.Itoa(n)
}

// loopBodyBreakNeedsLabel reports whether `body` contains a `break`
// reachable only by crossing a `match`/`select` (a Go switch/select)
// without first entering a nested loop — i.e. a break the lowered
// switch would capture. It mirrors, statically, the runtime switchDepth
// test in breakTarget.
func loopBodyBreakNeedsLabel(body *ast.Block) bool {
	if body == nil {
		return false
	}
	return scanBlockForCapturedBreak(body, false)
}

func scanBlockForCapturedBreak(b *ast.Block, inSwitch bool) bool {
	if b == nil {
		return false
	}
	for _, st := range b.Stmts {
		if scanStmtForCapturedBreak(st, inSwitch) {
			return true
		}
	}
	return scanExprForCapturedBreak(b.Trailing, inSwitch)
}

func scanStmtForCapturedBreak(st ast.Stmt, inSwitch bool) bool {
	switch s := st.(type) {
	case *ast.ExprStmt:
		return scanExprForCapturedBreak(s.Expr, inSwitch)
	case *ast.IfStmt:
		if scanBlockForCapturedBreak(s.ThenBlock, inSwitch) {
			return true
		}
		return scanElseForCapturedBreak(s.Else, inSwitch)
	case *ast.SelectStmt:
		// A Go `select` captures `break` just like a switch.
		for _, c := range s.Cases {
			if scanBlockForCapturedBreak(selectCaseBody(c), true) {
				return true
			}
		}
		return false
	case *ast.ForStmt, *ast.WhileStmt:
		// A nested loop owns its own breaks — do not descend.
		return false
	}
	// let/var/assign initialisers are value position: a `match`/block
	// there lowers to an IIFE (emitMatchAsExpr), which does not bump the
	// switch depth, so a `break` inside cannot reach the loop by label
	// anyway (it is trapped in the func literal — a separate sema gap).
	// Not descending keeps the scanner in lockstep with the runtime
	// switch-depth test (only statement/tail matches enterSwitch).
	return false
}

func scanElseForCapturedBreak(else_ ast.Node, inSwitch bool) bool {
	switch e := else_.(type) {
	case *ast.IfStmt:
		return scanStmtForCapturedBreak(e, inSwitch)
	case *ast.Block:
		return scanBlockForCapturedBreak(e, inSwitch)
	}
	return false
}

func scanExprForCapturedBreak(e ast.Expr, inSwitch bool) bool {
	switch x := e.(type) {
	case *ast.BreakExpr:
		return inSwitch
	case *ast.MatchExpr:
		// A `match` lowers to a Go `switch` — arms run in switch context.
		for _, arm := range x.Arms {
			if scanExprForCapturedBreak(arm.Body, true) {
				return true
			}
		}
		return false
	case *ast.IfExpr:
		if scanBlockForCapturedBreak(x.ThenBlock, inSwitch) {
			return true
		}
		return scanElseForCapturedBreak(x.Else, inSwitch)
	case *ast.Block:
		return scanBlockForCapturedBreak(x, inSwitch)
	}
	return false
}

func selectCaseBody(c ast.SelectCase) *ast.Block {
	switch v := c.(type) {
	case *ast.SelectRecv:
		return v.Body
	case *ast.SelectSend:
		return v.Body
	case *ast.SelectDefault:
		return v.Body
	}
	return nil
}
