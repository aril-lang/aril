// Package codegen lowers the type-checked Tide AST to Go source.
//
// Go is an intermediate representation (decision D1 in AI.md): generated Go
// need not be readable. Codegen emits //line directives so panics, stack
// traces, delve and pprof map back to .td source (decision D8).
//
// Status: not implemented. See TODO.md and docs/architecture.md.
package codegen
