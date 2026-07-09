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

// TestHTTPResponseFields locks the handle *field* axis (HTTP-CLIENT epoch): the
// first handle to expose struct fields, not just methods. http.Response fields
// resolve to their exported Go name + Aril type spelling; an unbound field and a
// non-field-bearing handle both miss.
func TestHTTPResponseFields(t *testing.T) {
	ht, ok := HandleTypeOf("http.Response")
	if !ok || ht.GoType != "*http.Response" || ht.GoPkg != "http" {
		t.Fatalf("http.Response lowering = %+v ok=%v; want *http.Response / http", ht, ok)
	}
	cases := map[string][2]string{ // arilName → {GoName, Return}
		"statusCode": {"StatusCode", "int"},
		"status":     {"Status", "string"},
		"header":     {"Header", "http.Header"},
		"body":       {"Body", "io.ReadCloser"},
	}
	for aril, want := range cases {
		f, ok := HandleFieldOf("http.Response", aril)
		if !ok {
			t.Fatalf("http.Response.%s should be a bound field", aril)
		}
		if f.GoName != want[0] || f.Return != want[1] {
			t.Errorf("http.Response.%s = %+v; want %s → %s", aril, f, want[0], want[1])
		}
	}
	if _, ok := HandleFieldOf("http.Response", "nope"); ok {
		t.Error("http.Response.nope should not be a bound field")
	}
	// A method-only handle has no field axis (regexp.Regexp is not field-bearing).
	if _, ok := HandleFieldOf("regexp.Regexp", "statusCode"); ok {
		t.Error("regexp.Regexp has no bound fields")
	}
}

// TestHTTPHeaderMethods locks the http.Header method-only handle: Go's http.Header
// is a map type with value methods, and Aril `delete` maps to Go's `Del` (not
// Delete). Header carries no field axis.
func TestHTTPHeaderMethods(t *testing.T) {
	ht, ok := HandleTypeOf("http.Header")
	if !ok || ht.GoType != "http.Header" || ht.GoPkg != "http" {
		t.Fatalf("http.Header lowering = %+v ok=%v; want http.Header / http", ht, ok)
	}
	get, _ := HandleMethodOf("http.Header", "get")
	if get.GoName != "Get" || get.Return != "string" {
		t.Errorf("http.Header.get = %+v; want Get → string", get)
	}
	del, ok := HandleMethodOf("http.Header", "delete")
	if !ok || del.GoName != "Del" {
		t.Errorf("http.Header.delete = %+v; want Del (Go's method is Del, not Delete)", del)
	}
	values, _ := HandleMethodOf("http.Header", "values")
	if values.GoName != "Values" || values.Return != "[]string" {
		t.Errorf("http.Header.values = %+v; want Values → []string", values)
	}
	set, _ := HandleMethodOf("http.Header", "set")
	if set.GoName != "Set" || set.Return != "unit" {
		t.Errorf("http.Header.set = %+v; want Set → unit", set)
	}
	add, _ := HandleMethodOf("http.Header", "add")
	if add.GoName != "Add" || add.Return != "unit" {
		t.Errorf("http.Header.add = %+v; want Add → unit", add)
	}
	if _, ok := HandleFieldOf("http.Header", "get"); ok {
		t.Error("http.Header is method-only; it has no field axis")
	}
}

// TestHTTPRequestDivergentURLField locks the first *divergent-name* handle field:
// http.Request.url → Go `URL` (exportFieldName would give `Url`), yielding a
// url.URL handle so `r.url.path` chains; `method` is the identical-capitalization
// control.
func TestHTTPRequestDivergentURLField(t *testing.T) {
	u, ok := HandleFieldOf("http.Request", "url")
	if !ok || u.GoName != "URL" || u.Return != "url.URL" {
		t.Errorf("http.Request.url = %+v ok=%v; want URL → url.URL (divergent from capitalize=Url)", u, ok)
	}
	if u.GoName == "Url" {
		t.Error("http.Request.url must NOT be exportFieldName(url)=Url — the whole point is the divergent Go name URL")
	}
	m, _ := HandleFieldOf("http.Request", "method")
	if m.GoName != "Method" || m.Return != "string" {
		t.Errorf("http.Request.method = %+v; want Method → string", m)
	}
}

// TestURLHandle locks the net/url URL handle: a pointer handle with a field axis
// (scheme/host/path, capitalizing identically) plus a string() method.
func TestURLHandle(t *testing.T) {
	ht, ok := HandleTypeOf("url.URL")
	if !ok || ht.GoType != "*url.URL" || ht.GoPkg != "url" {
		t.Fatalf("url.URL lowering = %+v ok=%v; want *url.URL / url", ht, ok)
	}
	for aril, goName := range map[string]string{"scheme": "Scheme", "host": "Host", "path": "Path", "rawQuery": "RawQuery", "fragment": "Fragment"} {
		f, ok := HandleFieldOf("url.URL", aril)
		if !ok || f.GoName != goName || f.Return != "string" {
			t.Errorf("url.URL.%s = %+v ok=%v; want %s → string", aril, f, ok, goName)
		}
	}
	s, ok := HandleMethodOf("url.URL", "string")
	if !ok || s.GoName != "String" || s.Return != "string" {
		t.Errorf("url.URL.string = %+v ok=%v; want String → string", s, ok)
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

// TestSyncHandles locks the sync primitives (IDIOM-CLOSURE): Mutex/WaitGroup
// are Constructable value handles (built with `pkg.Type{}`, unlike every
// obtain-only handle) whose methods rename to the Go set. lock/unlock/wait/done
// return unit; tryLock returns bool; add takes an int.
func TestSyncHandles(t *testing.T) {
	for _, spelled := range []string{"sync.Mutex", "sync.WaitGroup"} {
		ht, ok := HandleTypeOf(spelled)
		if !ok {
			t.Fatalf("%s should be a handle type", spelled)
		}
		if ht.GoType != spelled || ht.GoPkg != "sync" {
			t.Errorf("%s lowering = %+v; want value GoType %s / sync", spelled, ht, spelled)
		}
		if !ht.Constructable || !IsConstructableHandle(spelled) {
			t.Errorf("%s should be Constructable (built with %s{})", spelled, spelled)
		}
	}
	// Obtain-only handles must stay non-constructable so a brace literal on them
	// is rejected, not silently zero-built.
	if IsConstructableHandle("regexp.Regexp") || IsConstructableHandle("net.Conn") {
		t.Error("regexp.Regexp / net.Conn are obtain-only, not constructable")
	}
	lock, _ := HandleMethodOf("sync.Mutex", "lock")
	if lock.GoName != "Lock" || lock.Return != "unit" {
		t.Errorf("sync.Mutex.lock = %+v; want Lock → unit", lock)
	}
	tryLock, _ := HandleMethodOf("sync.Mutex", "tryLock")
	if tryLock.GoName != "TryLock" || tryLock.Return != "bool" {
		t.Errorf("sync.Mutex.tryLock = %+v; want TryLock → bool", tryLock)
	}
	add, _ := HandleMethodOf("sync.WaitGroup", "add")
	if add.GoName != "Add" || len(add.Params) != 1 || add.Params[0] != "int" {
		t.Errorf("sync.WaitGroup.add = %+v; want Add(int)", add)
	}
	wait, _ := HandleMethodOf("sync.WaitGroup", "wait")
	if wait.GoName != "Wait" || wait.Return != "unit" {
		t.Errorf("sync.WaitGroup.wait = %+v; want Wait → unit", wait)
	}
}

// TestAtomicHandles locks the sync/atomic scalar cells (ATOMICS-BINDING):
// atomic.Int64/Uint64/Bool are Constructable value handles (like sync.Mutex —
// built `atomic.T{}`, ready to use at zero) whose methods rename to the Go set.
// They map to the Aril import name `atomic` (→ sync/atomic via goImportPath), so
// GoPkg is "atomic", not the import path. load/store/swap/compareAndSwap are
// common; add is integer-only (Bool has none).
func TestAtomicHandles(t *testing.T) {
	for _, spelled := range []string{"atomic.Int64", "atomic.Uint64", "atomic.Bool"} {
		ht, ok := HandleTypeOf(spelled)
		if !ok {
			t.Fatalf("%s should be a handle type", spelled)
		}
		if ht.GoType != spelled || ht.GoPkg != "atomic" {
			t.Errorf("%s lowering = %+v; want value GoType %s / atomic", spelled, ht, spelled)
		}
		if !ht.Constructable || !IsConstructableHandle(spelled) {
			t.Errorf("%s should be Constructable (built with %s{})", spelled, spelled)
		}
		// The shared method set, present on all three.
		for _, m := range []string{"load", "store", "swap", "compareAndSwap"} {
			if _, ok := HandleMethodOf(spelled, m); !ok {
				t.Errorf("%s should bind method %s", spelled, m)
			}
		}
	}
	// Int64: load→int64, store(int64)→unit, compareAndSwap(int64,int64)→bool, add(int64)→int64.
	load, _ := HandleMethodOf("atomic.Int64", "load")
	if load.GoName != "Load" || load.Return != "int64" || len(load.Params) != 0 {
		t.Errorf("atomic.Int64.load = %+v; want Load() → int64", load)
	}
	store, _ := HandleMethodOf("atomic.Int64", "store")
	if store.GoName != "Store" || store.Return != "unit" || len(store.Params) != 1 || store.Params[0] != "int64" {
		t.Errorf("atomic.Int64.store = %+v; want Store(int64) → unit", store)
	}
	cas, _ := HandleMethodOf("atomic.Int64", "compareAndSwap")
	if cas.GoName != "CompareAndSwap" || cas.Return != "bool" || len(cas.Params) != 2 {
		t.Errorf("atomic.Int64.compareAndSwap = %+v; want CompareAndSwap(int64,int64) → bool", cas)
	}
	add, _ := HandleMethodOf("atomic.Int64", "add")
	if add.GoName != "Add" || add.Return != "int64" {
		t.Errorf("atomic.Int64.add = %+v; want Add(int64) → int64", add)
	}
	// Bool carries no add (integer-only operation).
	if _, ok := HandleMethodOf("atomic.Bool", "add"); ok {
		t.Error("atomic.Bool should not bind add (integer-only)")
	}
	boolLoad, _ := HandleMethodOf("atomic.Bool", "load")
	if boolLoad.Return != "bool" {
		t.Errorf("atomic.Bool.load = %+v; want → bool", boolLoad)
	}
	// atomic is an importable builtin module (stdlib category, not runtime).
	if !IsBuiltinModule("atomic") || !IsStdlibNamespace("atomic") {
		t.Error("atomic should be an importable stdlib builtin module")
	}
}

// TestContextHandle locks context.Context: an obtain-only interface handle
// (not Constructable) whose done() renames to Done() returning a receive
// channel of unit (Go's <-chan struct{}).
func TestContextHandle(t *testing.T) {
	ht, ok := HandleTypeOf("context.Context")
	if !ok || ht.GoType != "context.Context" || ht.GoPkg != "context" {
		t.Fatalf("context.Context lowering = %+v ok=%v; want bare interface / context", ht, ok)
	}
	if IsConstructableHandle("context.Context") {
		t.Error("context.Context is obtain-only, not constructable")
	}
	done, _ := HandleMethodOf("context.Context", "done")
	if done.GoName != "Done" || done.Return != "RecvChan<unit>" {
		t.Errorf("context.Context.done = %+v; want Done → RecvChan<unit>", done)
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

// TestHTTPServerHandle: http.Server is the first constructable handle with init
// fields (binding-surface §net/http). It is a pointer handle (*http.Server) so
// the literal lowers to `&http.Server{…}`; `handler`/`addr` are settable at
// construction; serve/shutdown wrap a bare-error Go return via ResultUnit.
func TestHTTPServerHandle(t *testing.T) {
	ht, ok := HandleTypeOf("http.Server")
	if !ok {
		t.Fatal("http.Server should be a handle type")
	}
	if ht.GoType != "*http.Server" || ht.GoPkg != "http" {
		t.Errorf("http.Server lowering = %+v; want *http.Server / http", ht)
	}
	if !ht.Constructable || !IsConstructableHandle("http.Server") {
		t.Error("http.Server should be Constructable")
	}
	// Init fields: handler → Handler, addr → Addr; an unknown field is absent.
	init, ok := HandleInitFieldsOf("http.Server")
	if !ok {
		t.Fatal("http.Server should have init fields")
	}
	if h := init["handler"]; h.GoName != "Handler" {
		t.Errorf("http.Server init handler = %+v; want Handler", h)
	}
	if a := init["addr"]; a.GoName != "Addr" {
		t.Errorf("http.Server init addr = %+v; want Addr", a)
	}
	if _, bad := init["bogus"]; bad {
		t.Error("http.Server should have no `bogus` init field")
	}
	// A fieldless constructable handle has no init-field spec.
	if _, ok := HandleInitFieldsOf("sync.Mutex"); ok {
		t.Error("sync.Mutex is fieldless zero-construction, should have no init fields")
	}
	// Methods.
	serve, _ := HandleMethodOf("http.Server", "serve")
	if serve.GoName != "Serve" || serve.Return != "Result<unit, error>" {
		t.Errorf("http.Server.serve = %+v; want Serve → Result<unit, error>", serve)
	}
	shut, _ := HandleMethodOf("http.Server", "shutdown")
	if shut.GoName != "Shutdown" || shut.Return != "Result<unit, error>" {
		t.Errorf("http.Server.shutdown = %+v; want Shutdown → Result<unit, error>", shut)
	}
}
