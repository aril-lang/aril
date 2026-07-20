package main

import (
	"strings"
	"testing"
)

// explainTrace reframes a Go panic block into a native Aril trace: it
// translates the runtime message, keeps only `.aril` (user) frames with
// their native coordinates, prettifies each symbol, and drops the
// runtime/arilrt frames + `goroutine`/PC/`exit status` noise. Non-panic
// input passes through unchanged. (docs/aril-explain.md, v0.)
func TestExplainTrace(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   []string // substrings that must appear
		absent []string // substrings that must NOT appear
	}{
		{
			name: "integer divide by zero",
			in: `panic: runtime error: integer divide by zero

goroutine 1 [running]:
main.main()
	/tmp/x/p1.aril:8 +0x9
exit status 2
`,
			want:   []string{"panic: division by zero", "at main", "(p1.aril:8)"},
			absent: []string{"runtime error", "goroutine", "exit status", "+0x9"},
		},
		{
			name: "index out of range, multi-frame",
			in: `panic: runtime error: index out of range [99] with length 3

goroutine 1 [running]:
main.deeper(...)
	/tmp/x/mf.aril:4
main.middle(...)
	/tmp/x/mf.aril:7
main.main()
	/tmp/x/mf.aril:12 +0x19
exit status 2
`,
			want: []string{
				"panic: index out of range [99], length 3",
				"at deeper", "(mf.aril:4)",
				"at middle", "(mf.aril:7)",
				"at main", "(mf.aril:12)",
			},
			absent: []string{"with length", "goroutine"},
		},
		{
			name: "user method, arilrt frame hidden",
			in: `panic: runtime error: index out of range [50] with length 2

goroutine 1 [running]:
aril-output/arilrt.(*List[...]).At(...)
	/tmp/x/aril-out/gen/arilrt/containers.go:183
main.(*Grid).at(...)
	/tmp/x/um.aril:8
main.main()
	/tmp/x/um.aril:13 +0xcc
exit status 2
`,
			want: []string{
				"at Grid.at", "(um.aril:8)",
				"at main", "(um.aril:13)",
				"1 internal frame(s) hidden",
			},
			absent: []string{"arilrt", "containers.go", "(*List"},
		},
		{
			name: "nil dereference with signal line",
			in: `panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x488a3b]

goroutine 1 [running]:
main.main()
	/tmp/x/nd.aril:21 +0xdb
exit status 2
`,
			want:   []string{"panic: nil dereference", "at main", "(nd.aril:21)"},
			absent: []string{"SIGSEGV", "invalid memory address"},
		},
		{
			name:   "send on closed channel",
			in:     "panic: send on a closed channel\n\ngoroutine 1 [running]:\nmain.main()\n\t/tmp/x/sc.aril:7 +0x1\nexit status 2\n",
			want:   []string{"panic: send on a closed channel", "at main", "(sc.aril:7)"},
			absent: []string{"goroutine"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := explainTrace(tc.in)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("unexpected %q in:\n%s", a, got)
				}
			}
		})
	}
}

// Non-panic input is echoed unchanged — the filter is never worse than the
// raw stream.
func TestExplainTracePassthrough(t *testing.T) {
	in := "hello\nworld\nregular program output\n"
	if got := explainTrace(in); got != in {
		t.Errorf("passthrough altered non-panic input:\ngot:  %q\nwant: %q", got, in)
	}
}

// A method whose name starts with `func` (e.g. `funcDecl`) is NOT a
// closure — the closure heuristic keys on `.funcN` (dot + digit), so it
// renders as a normal method, while a real `.func1` closure gets the label.
func TestExplainTraceFuncNamedMethodVsClosure(t *testing.T) {
	in := `panic: runtime error: nil dereference

goroutine 1 [running]:
main.(*Parser).funcDecl(...)
	/tmp/x/parse.aril:12
main.main.func1()
	/tmp/x/parse.aril:30
main.main()
	/tmp/x/parse.aril:31 +0x1
`
	got := explainTrace(in)
	if !strings.Contains(got, "at Parser.funcDecl") {
		t.Errorf("a method named funcDecl must not be labeled a closure:\n%s", got)
	}
	if !strings.Contains(got, "<closure>") {
		t.Errorf("a real .func1 frame must get the closure label:\n%s", got)
	}
}

// A `created by … in goroutine N` spawn-site frame is kept (T3 concurrency
// debugging) and reads "spawned at", not "at".
func TestExplainTraceSpawnSite(t *testing.T) {
	in := `panic: runtime error: index out of range [3] with length 2

goroutine 34 [running]:
main.worker(...)
	/tmp/x/pool.aril:8
created by main.run in goroutine 1
	/tmp/x/pool.aril:20 +0x5
`
	got := explainTrace(in)
	if !strings.Contains(got, "at worker") || !strings.Contains(got, "(pool.aril:8)") {
		t.Errorf("panicking frame lost:\n%s", got)
	}
	if !strings.Contains(got, "spawned at run") || !strings.Contains(got, "(pool.aril:20)") {
		t.Errorf("spawn-site frame lost or mislabeled:\n%s", got)
	}
}

// A panic whose whole stack is internal (no `.aril` frame) says so rather
// than printing an empty trace.
func TestExplainTraceAllInternal(t *testing.T) {
	in := `panic: runtime error: integer divide by zero

goroutine 1 [running]:
aril-output/arilrt.doThing(...)
	/tmp/x/aril-out/gen/arilrt/runtime.go:9
exit status 2
`
	got := explainTrace(in)
	if !strings.Contains(got, "panic: division by zero") {
		t.Errorf("want translated message, got:\n%s", got)
	}
	if !strings.Contains(got, "no Aril frames") {
		t.Errorf("want the no-Aril-frames note, got:\n%s", got)
	}
}
