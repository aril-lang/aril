package binding

// Manifest is the curated list of stdlib symbols the corpus's builtin-module
// surface binds — keyed by Go import path, valued by the exported Go symbol
// names. This is the *human-owned* half of D6 ("which symbols"); the binding
// *signatures* for these symbols are derived mechanically from go/types by the
// bindgen deriver. Grow a row when the corpus calls a new mechanical stdlib
// binding; the idiom bindings (fmt.scan*, json, sort.sorted, time-duration
// constructors, strings.fromBytes) are not listed here — they are hand-authored
// wrappers in the consumers, not mechanical signature transforms.
//
// The deriver (internal/bindgen) renders registry_gen.go from this Manifest;
// the drift-guard test fails if the committed registry no longer matches a
// fresh derivation.
//
// Curation note — `fmt.Print/Printf/Println` are deliberately NOT listed. Their
// Go referents return `(int, error)`, so a mechanical derivation would lift them
// to `Result<int, error>` and wrap every call in `ResultOf` — but the Aril
// surface treats them as fire-and-forget *effects* that discard the count+error.
// That discard is a curation choice, not a signature transform, so they stay in
// the codegen idiom overlay (the `(int, error)`-as-effect being exactly the
// "semantic layer needs review" carve-out D6 names). The `Sprint*` variants
// return a bare `string` and are mechanical, so they are listed.
var Manifest = map[string][]string{
	"errors":  {"Is"},
	"fmt":     {"Sprint", "Sprintf", "Sprintln"},
	"os":      {"Args", "Exit", "Getenv", "ReadFile", "WriteFile"},
	"strings": {"Contains", "Count", "Fields", "HasPrefix", "HasSuffix", "Join", "Replace", "Split", "ToLower", "ToUpper", "TrimPrefix", "TrimSpace", "TrimSuffix"},
	"strconv": {"Atoi", "FormatBool", "FormatFloat", "Itoa", "ParseBool", "ParseFloat", "ParseInt", "Quote"},
	"math":    {"Abs", "Ceil", "Floor", "Log", "Log10", "Log2", "Max", "Min", "Pow", "Sqrt"},
	"time":    {"After", "Sleep", "Tick"},
	"unicode": {"IsDigit", "IsLetter", "IsSpace"},
}

// Curation note — `errors.New` and `fmt.Errorf` are deliberately NOT listed
// here, though they return a bare `error`. The mechanical deriver lifts a
// bare-`error` result to `Result<unit, error>` (the failure-signal reading,
// correct for effects like `os.WriteFile` / `os.Chdir`). But `errors.New` /
// `fmt.Errorf` are error *constructors* — the returned `error` IS the value, not
// a failure signal — so they must stay a bare-`error` Rename in the idiom
// overlay (the codegen `stdlibRenameOverlay`), not a registry ResultWrap row.
