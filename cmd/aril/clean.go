package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdClean removes a project's build-artifact directory — the counterpart to
// the persisted `aril-out/` layout (RFC-0009 §Cleaning). With no selector it
// removes the whole out-dir; `--gen` / `--bin` remove only those sub-trees.
// The out-dir is resolved exactly as build/run resolve it (--out-dir › ARIL_OUT
// › [build] out-dir › ./aril-out), including the per-project segment under a
// shared out-dir — so `clean` on a shared dir removes *this* project's segment,
// never a sibling's.
func cmdClean(args []string) int {
	fs := flag.NewFlagSet("aril clean", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDirFlag := fs.String("out-dir", "", "build-artifact directory (default: ./aril-out; RFC-0009)")
	genOnly := fs.Bool("gen", false, "remove only the lowered Go (aril-out/gen)")
	binOnly := fs.Bool("bin", false, "remove only the compiled binaries (aril-out/bin)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril clean [-gen] [-bin] [-out-dir <dir>] [<dir>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "aril clean: expected at most one <dir>")
		return 2
	}
	// The project to clean; defaults to the cwd (its manifest, or the cwd itself,
	// is the resolution root).
	target := "."
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	outDir, err := resolveOutDir(target, *outDirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Selectors compose: --gen and/or --bin remove those sub-trees; neither
	// removes the whole out-dir.
	var targets []string
	switch {
	case *genOnly && *binOnly:
		targets = []string{filepath.Join(outDir, "gen"), filepath.Join(outDir, "bin")}
	case *genOnly:
		targets = []string{filepath.Join(outDir, "gen")}
	case *binOnly:
		targets = []string{filepath.Join(outDir, "bin")}
	default:
		targets = []string{outDir}
	}
	for _, t := range targets {
		if err := os.RemoveAll(t); err != nil {
			fmt.Fprintf(os.Stderr, "aril clean: %v\n", err)
			return 1
		}
	}
	return 0
}
