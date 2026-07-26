package sema

import (
	"fmt"
	"sort"
)

// Severity classifies a Diag as a blocking error or a non-blocking hint.
// The zero value is SeverityError, so every diagnostic built without an
// explicit severity (all E-code diagnostics) stays a blocking error — the
// hint tier is strictly additive (D58). A hint never fails the build or
// changes the exit code; it teaches just-in-time.
type Severity uint8

const (
	SeverityError Severity = iota // blocking; the default (zero value)
	SeverityHint                  // non-blocking teaching note (D58, the H-codes)
)

// Diag mirrors lexer/parser Diag shape so the CLI can print
// sema errors in the same `<file>:<line>:<col>: error[code]: msg`
// format. Codes come from lang-spec/diagnostics.md.
type Diag struct {
	File     string
	Code     string
	Message  string
	Line     int
	Col      int
	Severity Severity
}

// IsHint reports whether this diagnostic is a non-blocking hint (D58).
func (d *Diag) IsHint() bool { return d.Severity == SeverityHint }

func (d *Diag) Error() string {
	kind := "error"
	if d.Severity == SeverityHint {
		kind = "hint"
	}
	if d.File == "" {
		return fmt.Sprintf("%d:%d: %s[%s]: %s", d.Line, d.Col, kind, d.Code, d.Message)
	}
	return fmt.Sprintf("%s:%d:%d: %s[%s]: %s", d.File, d.Line, d.Col, kind, d.Code, d.Message)
}

// sortDiags by (file, line, col, code). See docs/internals/sema.md §8
// #4. File is the primary key so a multi-file package (RFC-0002) reports
// each file's diagnostics together; it is a no-op for a single file.
func sortDiags(ds []*Diag) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].File != ds[j].File {
			return ds[i].File < ds[j].File
		}
		if ds[i].Line != ds[j].Line {
			return ds[i].Line < ds[j].Line
		}
		if ds[i].Col != ds[j].Col {
			return ds[i].Col < ds[j].Col
		}
		return ds[i].Code < ds[j].Code
	})
}
