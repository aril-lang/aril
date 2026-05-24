// Command tide is the compiler and toolchain for the Tide programming language.
//
// Tide is pre-alpha. This entry point is a scaffold: subcommands are wired up
// but not yet implemented. See TODO.md for the immediate work.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Printf("tide %s\n", version)
	case "build":
		notImplemented("build", "TODO.md")
	case "run":
		notImplemented("run", "TODO.md")
	case "bindgen":
		notImplemented("bindgen", "backlog.md")
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "tide: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func notImplemented(cmd, where string) {
	fmt.Fprintf(os.Stderr, "tide %s: not implemented yet (see %s)\n", cmd, where)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Tide - modern TypeScript-style syntax on the Go runtime.

Usage:
  tide <command> [arguments]

Commands:
  build      compile a .td file or package to a native binary
  run        compile and run a .td program
  bindgen    generate Tide bindings from a Go package
  version    print the compiler version
  help       print this message

Status: pre-alpha. See TODO.md.`)
}
