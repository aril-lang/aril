package binding

// membership.go — the consolidated stdlib binding-membership surface: the
// idiom-row *value* tables sema and codegen share, plus IsMember (the folded
// bound set sema's unbound-member diagnostic reads). Rationale + the lockstep
// invariant: docs/architecture.md §binding subsystem. Surface: binding-surface.md.

// renameOverlay — value/effect renames the derived registry excludes as a
// curation choice (fire-and-forget effects, value-returning `error` ctors,
// generic slices helpers). binding-surface.md §curation.
var renameOverlay = map[[2]string]string{
	{"fmt", "println"}:     "Println",
	{"fmt", "print"}:       "Print",
	{"fmt", "printf"}:      "Printf",
	{"log", "println"}:     "Println", // fire-and-forget effects (§log)
	{"log", "printf"}:      "Printf",
	{"log", "print"}:       "Print",
	{"log", "fatal"}:       "Fatal",
	{"log", "fatalf"}:      "Fatalf",
	{"log", "setPrefix"}:   "SetPrefix",
	{"log", "setFlags"}:    "SetFlags",
	{"errors", "new"}:      "New",    // value-returning error ctor, not a ResultWrap (§errors)
	{"fmt", "errorf"}:      "Errorf", // adds %w wrapping (§fmt)
	{"slices", "max"}:      "Max",    // generic; Go infers the element type (§slices)
	{"slices", "min"}:      "Min",
	{"slices", "contains"}: "Contains",
	{"slices", "indexOf"}:  "Index",
	{"os", "stdin"}:        "Stdin", // value ref (os.Stdin, an io.Reader) — bufio.newScanner consumes it
}

// RenameOverlayOf returns the Go identifier for an overlay rename `pkg.arilName`,
// or ("", false) when the pair is not an overlay row.
func RenameOverlayOf(pkg, arilName string) (string, bool) {
	g, ok := renameOverlay[[2]string{pkg, arilName}]
	return g, ok
}

// conversionTable — bindings lowering to a Go type conversion `target(arg)`,
// not a package call (no import). Single source, else imports mis-track (§strings).
var conversionTable = map[[2]string]string{
	{"strings", "fromBytes"}: "string", // []byte → string
	{"strings", "toBytes"}:   "[]byte", // string → []byte
}

// ConversionOf returns the Go conversion target for a conversion binding
// `pkg.arilName`, or ("", false) when the pair is not one.
func ConversionOf(pkg, arilName string) (string, bool) {
	g, ok := conversionTable[[2]string{pkg, arilName}]
	return g, ok
}

// commaOkTable — Go `(T, bool)` referents lowering to `OptionOf(...)` (§os).
var commaOkTable = map[[2]string]string{
	{"os", "lookupEnv"}: "LookupEnv", // (string, bool) → Option<string>
}

// CommaOkOf returns the Go identifier for a comma-ok binding `pkg.arilName`, or
// ("", false) when the pair is not one.
func CommaOkOf(pkg, arilName string) (string, bool) {
	g, ok := commaOkTable[[2]string{pkg, arilName}]
	return g, ok
}

// DurationUnitOf maps a `time.<ctor>(n)` Duration constructor to its Go
// `time.<Unit>` constant (call lowers to `time.Duration(n) * time.<Unit>`, §time).
func DurationUnitOf(arilName string) (string, bool) {
	switch arilName {
	case "seconds":
		return "Second", true
	case "milliseconds":
		return "Millisecond", true
	}
	return "", false
}

// idiomMembers — members whose lowering/typing is code (codegen intercepts /
// sema generic bindings), registering membership only. MUST stay in lockstep
// with those intercepts. Lockstep invariant + source map: architecture.md
// §binding subsystem.
var idiomMembers = map[[2]string]bool{
	{"json", "parse"}:           true,
	{"json", "serialize"}:       true,
	{"json", "serializeIndent"}: true,
	{"errors", "as"}:            true,
	{"fmt", "scan"}:             true,
	{"fmt", "scan2"}:            true,
	{"fmt", "scan3"}:            true,
	{"sort", "sorted"}:          true,
	{"sort", "sortedBy"}:        true,
	{"slices", "reverse"}:       true,
	{"slices", "dedup"}:         true,
	{"reflect", "box"}:          true,
	{"reflect", "unbox"}:        true,
	{"reflect", "typeOf"}:       true,
	{"reflect", "typeName"}:     true,
	{"reflect", "kind"}:         true,
	{"reflect", "fields"}:       true,
	{"reflect", "fieldValue"}:   true,
	{"reflect", "show"}:         true,
}

// IsMember reports whether `pkg.arilName` is bound, across every source
// (registry, overlays, handle ctors, idiom members). Sole consumer: sema's
// unbound-member diagnostic (D10) — sound-over-complete, architecture.md
// §binding subsystem.
func IsMember(pkg, arilName string) bool {
	if _, ok := registry[[2]string{pkg, arilName}]; ok {
		return true
	}
	if _, ok := renameOverlay[[2]string{pkg, arilName}]; ok {
		return true
	}
	if _, ok := conversionTable[[2]string{pkg, arilName}]; ok {
		return true
	}
	if _, ok := commaOkTable[[2]string{pkg, arilName}]; ok {
		return true
	}
	if pkg == "time" {
		if _, ok := DurationUnitOf(arilName); ok {
			return true
		}
	}
	if _, ok := handleCtors[[2]string{pkg, arilName}]; ok {
		return true
	}
	return idiomMembers[[2]string{pkg, arilName}]
}
