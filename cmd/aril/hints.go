package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aril-lang/aril/internal/sema"
)

// hintPolicy decides which non-blocking hints (D58) are shown. Hints never
// affect build success or the exit code; this only governs whether each is
// printed. The zero value shows every hint (default-on).
type hintPolicy struct {
	allOff bool            // suppress every hint (--hints=off / ARIL_HINTS=off)
	off    map[string]bool // suppress specific hint codes (ARIL_HINTS=off:H0001,…)
}

// show reports whether a hint with the given code should be printed.
func (h hintPolicy) show(code string) bool {
	if h.allOff {
		return false
	}
	return !h.off[code]
}

// addHintsFlag registers `--hints=<on|off>` on fs. The empty default means
// "unset" — resolveHintPolicy then defers to the ARIL_HINTS env var, so the
// flag overrides the environment (flag › env › default-on, mirroring
// resolveOutDir's precedence).
func addHintsFlag(fs *flag.FlagSet) *string {
	return fs.String("hints", "",
		"teaching-hint output: on (default) | off; overrides ARIL_HINTS")
}

// checkHintsMode validates a --hints value ("" means unset).
func checkHintsMode(mode string) error {
	switch mode {
	case "", "on", "off":
		return nil
	default:
		return fmt.Errorf("aril: unknown --hints mode %q (want on|off)", mode)
	}
}

// resolveHintPolicy computes the effective hint policy from the --hints flag
// (flagVal, "" when unset) and the ARIL_HINTS env var. Precedence: an explicit
// flag wins; otherwise the env is consulted; otherwise hints are on.
//
// ARIL_HINTS grammar: "off" suppresses every hint; "off:H0001,H0002" suppresses
// just those codes; "on" (or unset/empty) shows all.
func resolveHintPolicy(flagVal string) hintPolicy {
	switch flagVal {
	case "on":
		return hintPolicy{}
	case "off":
		return hintPolicy{allOff: true}
	}
	// Unset flag → defer to the environment.
	env := strings.TrimSpace(os.Getenv("ARIL_HINTS"))
	switch {
	case env == "" || env == "on":
		return hintPolicy{}
	case env == "off":
		return hintPolicy{allOff: true}
	case strings.HasPrefix(env, "off:"):
		off := make(map[string]bool)
		for _, code := range strings.Split(env[len("off:"):], ",") {
			if code = strings.TrimSpace(code); code != "" {
				off[code] = true
			}
		}
		return hintPolicy{off: off}
	default:
		// An unrecognized value is treated as "on" — hints are advisory, so
		// a typo must never silently blind the developer or fail the build.
		return hintPolicy{}
	}
}

// printDiags renders sema diagnostics to stderr and reports whether any
// blocking (error-severity) diagnostic was present. Hints are printed only
// when the policy shows them; a hint never sets blocking, so the caller must
// fail the build only on the returned value (D58: hints don't affect exit).
func printDiags(diags []*sema.Diag, hints hintPolicy) (blocking bool) {
	for _, d := range diags {
		if d.IsHint() {
			if hints.show(d.Code) {
				fmt.Fprintln(os.Stderr, d.Error())
			}
			continue
		}
		fmt.Fprintln(os.Stderr, d.Error())
		blocking = true
	}
	return blocking
}
