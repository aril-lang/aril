package binding

import "testing"

// TestHandleCtorLookup locks the value-handle constructor table: a handle
// constructor resolves to its Go builder + the handle's Aril type spelling, and
// a non-constructor pair misses.
func TestHandleCtorLookup(t *testing.T) {
	b, ok := HandleCtorOf("regexp", "mustCompile")
	if !ok {
		t.Fatal("regexp.mustCompile should be a handle constructor")
	}
	if b.GoName != "MustCompile" || b.Return != "regexp.Regexp" {
		t.Errorf("regexp.mustCompile = %+v; want MustCompile → regexp.Regexp", b)
	}
	if _, ok := HandleCtorOf("regexp", "nope"); ok {
		t.Error("regexp.nope should not be a handle constructor")
	}
}

// TestHandleMethodLookup locks the value-handle method table: a bound method
// resolves to its Go name + return spelling; an unbound method and an unknown
// handle both miss. IsHandleType agrees with the method table.
func TestHandleMethodLookup(t *testing.T) {
	m, ok := HandleMethodOf("regexp.Regexp", "findAll")
	if !ok {
		t.Fatal("regexp.Regexp.findAll should be bound")
	}
	if m.GoName != "FindAllString" || m.Return != "[]string" {
		t.Errorf("findAll = %+v; want FindAllString → []string", m)
	}
	if _, ok := HandleMethodOf("regexp.Regexp", "nope"); ok {
		t.Error("regexp.Regexp.nope should not be bound")
	}
	if _, ok := HandleMethodOf("os.File", "read"); ok {
		t.Error("os.File is not a bound handle type")
	}
	if !IsHandleType("regexp.Regexp") {
		t.Error("regexp.Regexp should be a handle type")
	}
	if IsHandleType("time.Time") {
		t.Error("time.Time is not (yet) a bound handle type")
	}
}

// TestHandleType locks the handle-type lowering: the Go type spelling is
// pointer-correct (regexp.MustCompile returns *regexp.Regexp) and carries the
// Go import package, both of which differ from the Aril spelling.
func TestHandleType(t *testing.T) {
	ht, ok := HandleTypeOf("regexp.Regexp")
	if !ok {
		t.Fatal("regexp.Regexp should be a handle type")
	}
	if ht.GoType != "*regexp.Regexp" {
		t.Errorf("regexp.Regexp GoType = %q; want *regexp.Regexp", ht.GoType)
	}
	if ht.GoPkg != "regexp" {
		t.Errorf("regexp.Regexp GoPkg = %q; want regexp", ht.GoPkg)
	}
	if _, ok := HandleTypeOf("time.Time"); ok {
		t.Error("time.Time is not (yet) a bound handle type")
	}
}

// TestRuntimeBackedHandle locks the big handle: it is runtime-backed (an arilrt
// BigInt wrapper, not a Go `big` package), its constructors lower to runtime
// helpers, and its method set is the functional arithmetic surface.
func TestRuntimeBackedHandle(t *testing.T) {
	ht, ok := HandleTypeOf("big.BigInt")
	if !ok || !ht.Runtime {
		t.Fatalf("big.BigInt should be a Runtime handle; got %+v ok=%v", ht, ok)
	}
	if ht.GoType != "BigInt" || ht.GoPkg != "" {
		t.Errorf("big.BigInt lowering = %+v; want GoType BigInt, no GoPkg", ht)
	}
	ctor, ok := HandleCtorOf("big", "fromInt")
	if !ok || ctor.GoName != "BigFromInt" || ctor.Return != "big.BigInt" {
		t.Errorf("big.fromInt = %+v; want BigFromInt → big.BigInt", ctor)
	}
	add, ok := HandleMethodOf("big.BigInt", "add")
	if !ok || add.GoName != "Add" || add.Return != "big.BigInt" {
		t.Errorf("big.BigInt.add = %+v; want Add → big.BigInt", add)
	}
	toI, _ := HandleMethodOf("big.BigInt", "toInt64")
	if toI.Return != "int64" {
		t.Errorf("big.BigInt.toInt64 return = %q; want int64", toI.Return)
	}
}

// TestBufioScannerHandle locks the bufio.Scanner handle: an external Go-package
// handle (like regexp) — the constructor consumes a reader and the method set
// is the line-by-line scan surface.
func TestBufioScannerHandle(t *testing.T) {
	ht, ok := HandleTypeOf("bufio.Scanner")
	if !ok || ht.Runtime {
		t.Fatalf("bufio.Scanner should be an external handle; got %+v ok=%v", ht, ok)
	}
	if ht.GoType != "*bufio.Scanner" || ht.GoPkg != "bufio" {
		t.Errorf("bufio.Scanner lowering = %+v; want *bufio.Scanner / bufio", ht)
	}
	ctor, ok := HandleCtorOf("bufio", "newScanner")
	if !ok || ctor.GoName != "NewScanner" || ctor.Return != "bufio.Scanner" {
		t.Errorf("bufio.newScanner = %+v; want NewScanner → bufio.Scanner", ctor)
	}
	scan, _ := HandleMethodOf("bufio.Scanner", "scan")
	text, _ := HandleMethodOf("bufio.Scanner", "text")
	if scan.GoName != "Scan" || scan.Return != "bool" {
		t.Errorf("bufio.Scanner.scan = %+v; want Scan → bool", scan)
	}
	if text.GoName != "Text" || text.Return != "string" {
		t.Errorf("bufio.Scanner.text = %+v; want Text → string", text)
	}
}

// TestDurationHandle locks the time.Duration handle: a scalar handle whose
// arithmetic methods (add/mul) return Duration but lower to Go operators (empty
// GoName), while string is a real method (→ String).
func TestDurationHandle(t *testing.T) {
	ht, ok := HandleTypeOf("time.Duration")
	if !ok || ht.GoType != "time.Duration" || ht.GoPkg != "time" {
		t.Fatalf("time.Duration lowering = %+v ok=%v; want time.Duration / time", ht, ok)
	}
	add, _ := HandleMethodOf("time.Duration", "add")
	mul, _ := HandleMethodOf("time.Duration", "mul")
	if add.Return != "time.Duration" || add.GoName != "" {
		t.Errorf("time.Duration.add = %+v; want empty GoName → time.Duration (operator-lowered)", add)
	}
	if mul.Return != "time.Duration" || mul.GoName != "" {
		t.Errorf("time.Duration.mul = %+v; want empty GoName → time.Duration (operator-lowered)", mul)
	}
	s, _ := HandleMethodOf("time.Duration", "string")
	if s.GoName != "String" || s.Return != "string" {
		t.Errorf("time.Duration.string = %+v; want String → string", s)
	}
}

// TestNetSocketHandles locks the net socket layer: Conn/Listener/Addr are
// external Go-interface handles (bare interface GoType, no pointer, GoPkg net),
// and the read/write/accept/close methods carry a Result<…> return — the first
// handle methods lifted via ResultOf/ResultUnit (net.Conn is io.Reader/Writer/
// Closer, so its byte-stream methods mirror Go's (T,error) / bare-error shapes).
func TestNetSocketHandles(t *testing.T) {
	for _, spelled := range []string{"net.Conn", "net.Listener", "net.Addr"} {
		ht, ok := HandleTypeOf(spelled)
		if !ok || ht.Runtime {
			t.Fatalf("%s should be an external handle; got %+v ok=%v", spelled, ht, ok)
		}
		if ht.GoType != spelled || ht.GoPkg != "net" {
			t.Errorf("%s lowering = %+v; want bare interface GoType %s / net", spelled, ht, spelled)
		}
	}
	// Conn: read/write are (int,error) → ResultOf; close is bare error → ResultUnit.
	read, _ := HandleMethodOf("net.Conn", "read")
	if read.GoName != "Read" || read.Return != "Result<int, error>" {
		t.Errorf("net.Conn.read = %+v; want Read → Result<int, error>", read)
	}
	write, _ := HandleMethodOf("net.Conn", "write")
	if write.GoName != "Write" || write.Return != "Result<int, error>" {
		t.Errorf("net.Conn.write = %+v; want Write → Result<int, error>", write)
	}
	cclose, _ := HandleMethodOf("net.Conn", "close")
	if cclose.GoName != "Close" || cclose.Return != "Result<unit, error>" {
		t.Errorf("net.Conn.close = %+v; want Close → Result<unit, error>", cclose)
	}
	// Listener: accept yields a Conn handle inside a Result; addr yields a plain
	// net.Addr handle (no Result — Go's Listener.Addr can't fail).
	accept, _ := HandleMethodOf("net.Listener", "accept")
	if accept.GoName != "Accept" || accept.Return != "Result<net.Conn, error>" {
		t.Errorf("net.Listener.accept = %+v; want Accept → Result<net.Conn, error>", accept)
	}
	addr, _ := HandleMethodOf("net.Listener", "addr")
	if addr.GoName != "Addr" || addr.Return != "net.Addr" {
		t.Errorf("net.Listener.addr = %+v; want Addr → net.Addr", addr)
	}
	lclose, _ := HandleMethodOf("net.Listener", "close")
	if lclose.GoName != "Close" || lclose.Return != "Result<unit, error>" {
		t.Errorf("net.Listener.close = %+v; want Close → Result<unit, error>", lclose)
	}
	astr, _ := HandleMethodOf("net.Addr", "string")
	if astr.GoName != "String" || astr.Return != "string" {
		t.Errorf("net.Addr.string = %+v; want String → string", astr)
	}
}

// TestNetDialListenRegistry locks the mechanical net constructors: net.dial /
// net.listen are derived ResultWrap rows (the deriver spells the local-package
// interface return net.Conn/net.Listener as a handle Named), NOT handle ctors.
func TestNetDialListenRegistry(t *testing.T) {
	dial, ok := ResultWrapOf("net", "dial")
	if !ok || dial != "Dial" {
		t.Errorf("net.dial ResultWrap = %q ok=%v; want Dial", dial, ok)
	}
	if s, _ := ReturnSpelling("net", "dial"); s != "Result<net.Conn, error>" {
		t.Errorf("net.dial return = %q; want Result<net.Conn, error>", s)
	}
	listen, ok := ResultWrapOf("net", "listen")
	if !ok || listen != "Listen" {
		t.Errorf("net.listen ResultWrap = %q ok=%v; want Listen", listen, ok)
	}
	if s, _ := ReturnSpelling("net", "listen"); s != "Result<net.Listener, error>" {
		t.Errorf("net.listen return = %q; want Result<net.Listener, error>", s)
	}
	// They are registry rows, not handle constructors.
	if _, ok := HandleCtorOf("net", "dial"); ok {
		t.Error("net.dial should be a registry ResultWrap row, not a handle ctor")
	}
}
