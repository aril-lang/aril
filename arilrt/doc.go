// Package arilrt is the Aril runtime: the small set of Go types and
// helpers the compiler's lowering depends on — Option / Result and their
// boundary lifts, the predeclared containers (Map / Set / Stack), the
// stdin scan helpers, structured-concurrency Group, the JSON
// round-trip, and the reflection layer (D18).
//
// It is the single source of truth for the runtime. Codegen emits one of
// two forms that both resolve to these definitions:
//
//   - vendored (default for build/run): the program imports this package
//     (qualified arilrt.X) and the build harness copies an embedded copy
//     of the package into the temp build module. Version-locked by
//     construction — the compiler binary embeds the runtime it emits
//     (CT2).
//   - inline single-file (--inline-runtime; default for emit): codegen
//     inlines only the used parts of these definitions into package main
//     with bare names, keeping emit a self-contained .go artifact.
//
// Per D18, the runtime surface is part of the language contract: the
// container internals are private (codegen may refactor their shape),
// while the reflection-facing surface (descriptors, the registry, the
// reflect.* functions) is a public commitment.
package arilrt
