package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// `aril explain` — reframe a Go panic block into a native Aril trace
// (docs/aril-explain.md). This is the v0 surface: a pure post-hoc text
// filter needing zero per-binary context — it reads a captured trace from
// stdin or a file, translates the runtime message, keeps only the user
// (`.aril`) frames with their already-native coordinates, prettifies each
// frame's symbol heuristically, and drops Go's `goroutine`/PC/`exit
// status` noise. A per-binary symbol sidecar (methods/generics/closures)
// is the v1 upgrade. If the input is not a recognisable panic, it is
// echoed unchanged (never worse than the raw trace).
func cmdExplain(args []string) int {
	fs := flag.NewFlagSet("aril explain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril explain [<file>]   (reads stdin when no file; e.g. `./prog 2>&1 | aril explain`)")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "aril explain: expected at most one <file>")
		return 2
	}

	var src io.Reader = os.Stdin
	if fs.NArg() == 1 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "aril explain:", err)
			return 1
		}
		defer f.Close()
		src = f
	}
	raw, err := io.ReadAll(bufio.NewReader(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "aril explain:", err)
		return 1
	}
	fmt.Print(explainTrace(string(raw)))
	return 0
}

// explainTrace is the pure translation core (unit-tested independently of
// the CLI). It returns the reframed trace, or the input unchanged when no
// `panic:` block is found.
func explainTrace(input string) string {
	lines := strings.Split(input, "\n")
	msgIdx := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "panic: ") {
			msgIdx = i
			break
		}
	}
	if msgIdx < 0 {
		return input // not a panic block — echo unchanged
	}

	message := translateMessage(strings.TrimPrefix(lines[msgIdx], "panic: "))
	frames, hiddenInternal := parseFrames(lines[msgIdx+1:])

	var b strings.Builder
	b.WriteString("panic: ")
	b.WriteString(message)
	b.WriteByte('\n')

	// Column-align the `(file:line)` suffix for readability.
	width := 0
	for _, f := range frames {
		if len(f.symbol) > width {
			width = len(f.symbol)
		}
	}
	for _, f := range frames {
		prefix := "at"
		if f.spawnSite {
			prefix = "spawned at"
		}
		fmt.Fprintf(&b, "  %s %-*s  (%s)\n", prefix, width, f.symbol, f.loc)
	}
	if len(frames) == 0 {
		// A recognised panic with no user frame (e.g. the crash is entirely
		// in the runtime/arilrt) — say so rather than print an empty trace.
		b.WriteString("  (no Aril frames — the panic is inside the runtime)\n")
	}
	if hiddenInternal > 0 {
		fmt.Fprintf(&b, "  … %d internal frame(s) hidden\n", hiddenInternal)
	}
	return b.String()
}

// arilFrame is one kept user frame: a prettified Aril symbol + its native
// `file:line`. spawnSite marks a `created by …` frame — the point a
// panicking goroutine was `spawn`ed from — so it reads "spawned at" rather
// than "at" (T3 concurrency debugging).
type arilFrame struct {
	symbol    string
	loc       string
	spawnSite bool
}

var (
	// A stack-frame location line: a leading tab, `<file>:<line>`, an
	// optional ` +0x…` PC offset. Go prints exactly this shape.
	locRe = regexp.MustCompile(`^\t(.+):(\d+)(?: \+0x[0-9a-fA-F]+)?$`)
	// A method symbol `pkg.(*Type).method` or `pkg.Type.method`.
	ptrRecvRe = regexp.MustCompile(`\(\*([^)]+)\)`)
	// A closure / IIFE / spawn-body frame: Go spells it `.funcN` (dot +
	// digit). Anchored on the digit so a method literally named `funcDecl`
	// is not mislabeled a closure.
	closureRe = regexp.MustCompile(`\.func\d`)
)

// parseFrames walks the goroutine trace, pairing each symbol line with its
// following location line, keeping only frames whose source is a `.aril`
// file (the user/internal discriminator is the path itself — see
// docs/aril-explain.md). It returns the kept frames in order and the count
// of dropped internal frames.
func parseFrames(lines []string) ([]arilFrame, int) {
	var frames []arilFrame
	hidden := 0
	for i := 0; i+1 < len(lines); i++ {
		sym := lines[i]
		if sym == "" || strings.HasPrefix(sym, "\t") {
			continue
		}
		// A `created by pkg.Fn in goroutine N` line is the spawn site of the
		// current goroutine — its symbol has no `name(...)` shape, so handle
		// it before the call-shape gate to keep its `.aril` coordinate.
		spawn := strings.HasPrefix(sym, "created by ")
		if !spawn && !strings.Contains(sym, "(") {
			continue // not a symbol line
		}
		m := locRe.FindStringSubmatch(lines[i+1])
		if m == nil {
			continue // not a frame pair
		}
		i++ // consume the location line
		file, line := m[1], m[2]
		if !strings.HasSuffix(file, ".aril") {
			hidden++ // runtime / arilrt / synthesised frame
			continue
		}
		if spawn {
			// `created by main.worker in goroutine 6` → the `main.worker` part.
			fn := strings.TrimPrefix(sym, "created by ")
			if j := strings.Index(fn, " in goroutine"); j >= 0 {
				fn = fn[:j]
			}
			frames = append(frames, arilFrame{
				symbol:    prettifyUserSymbol(fn),
				loc:       baseName(file) + ":" + line,
				spawnSite: true,
			})
			continue
		}
		frames = append(frames, arilFrame{
			symbol: prettifyUserSymbol(sym),
			loc:    baseName(file) + ":" + line,
		})
	}
	return frames, hidden
}

// prettifyUserSymbol renders a Go symbol from a user (package `main`) frame
// as an Aril name — a v0 heuristic, no symbol map: strip the `(...)` args
// and the `main.` package prefix, turn a `(*Type)` receiver into `Type.`,
// and mark a `.funcN` closure. A `goIdent`-mangled name (reserved-word
// collision, the `_arilSelf` family) is the case the v1 sidecar exists for;
// v0 leaves it as its cleaned Go spelling.
func prettifyUserSymbol(sym string) string {
	// Drop the trailing argument list `(0x.., …)` / `(...)`.
	if p := strings.LastIndex(sym, "("); p >= 0 {
		// Only trim if the '(' opens the arg list (not a `(*T)` receiver).
		if !strings.HasPrefix(sym[p:], "(*") {
			sym = sym[:p]
		}
	}
	sym = strings.TrimPrefix(sym, "main.")
	// `(*Grid).at` → `Grid.at`; a value receiver `Grid.at` is already fine.
	// Done before the closure check so a closure label reads cleanly
	// (`(*Grid).scan.func1` → `Grid.scan.func1`).
	sym = ptrRecvRe.ReplaceAllString(sym, "$1")
	// `main.func1` (closure / IIFE / spawn body) → a closure label.
	if closureRe.MatchString(sym) {
		return "<closure> (" + sym + ")"
	}
	if sym == "main" {
		return "main"
	}
	return sym
}

// translateMessage maps a Go runtime-error string to its Aril rendering.
// The set is small and closed; an unrecognised message passes through
// (with a leading `runtime error: ` stripped).
func translateMessage(msg string) string {
	m := strings.TrimSpace(msg)
	switch {
	case m == "runtime error: integer divide by zero":
		return "division by zero"
	case strings.HasPrefix(m, "runtime error: index out of range "):
		rest := strings.TrimPrefix(m, "runtime error: index out of range ")
		return "index out of range " + strings.Replace(rest, " with length ", ", length ", 1)
	case m == "runtime error: invalid memory address or nil pointer dereference":
		return "nil dereference"
	case m == "close of closed channel":
		return "channel already closed"
	case m == "send on closed channel":
		return "send on a closed channel"
	case m == "runtime error: negative shift amount":
		return "negative shift amount"
	case strings.HasPrefix(m, "runtime error: "):
		return strings.TrimPrefix(m, "runtime error: ")
	default:
		return m
	}
}

// baseName returns the final path segment (the `.aril` file name), so a
// frame reads `um.aril:8`, not an absolute build path.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}
