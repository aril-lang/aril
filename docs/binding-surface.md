# Binding surface (target sketch)

The intended Aril-side spelling of the Go-stdlib calls and types **used by
the v1 acceptance suite**, plus near-neighbours we expect to ship in the
first binding-generator iteration. This document is a **contract**: when
the binding generator (`internal/bindgen`) lands, the surface it produces
must match this sketch. The wrapper layer's design decisions — `(T,
error)` → `Result`, nullable-pointer → `Option`, non-nullable pointer →
direct, variadic `interface{}` → `...Any` — apply throughout.

> Note. Names follow the convention from `docs/language-spec.md`:
> exported Go identifiers are exposed in their lowerCamel form
> (`fmt.Println` → `fmt.println`); types keep PascalCase
> (`http.Handler`, `http.Request`).

## fmt

```aril
// Variadic output. Each argument widens to Any at the call site.
fmt.println(args: ...Any)
fmt.print(args: ...Any)
fmt.printf(format: string, args: ...Any)

// String-returning variants.
fmt.sprintf(format: string, args: ...Any): string
fmt.sprintln(args: ...Any): string
fmt.sprint(args: ...Any): string

// Error formatting (Go's fmt.Errorf with %w wrapping).
fmt.errorf(format: string, args: ...Any): error

// Writer-targeted variants used by HTTP and io examples.
fmt.fprintln(w: io.Writer, args: ...Any): Result<int, error>
fmt.fprintf(w: io.Writer, format: string, args: ...Any): Result<int, error>

// Stdin reading. Aril wraps Go's `fmt.Scan(&v)` (pointer-mutation
// style) into typed return form. `T` may be any of the numeric
// primitives (`int`, `int8..int64`, `uint..uint64`, `float32`,
// `float64`), `bool`, `byte`, `rune`, or `string` — anything Go's
// `fmt.Scan` knows how to parse from a single whitespace-separated
// token.
fmt.scan<T>(): Result<T, error>

// Multi-value forms — return a tuple so the call site can
// destructure with `let (a, b) = ...`. Provided for arities 2 and 3;
// for higher arities call `fmt.scan<T>()` repeatedly.
fmt.scan2<A, B>(): Result<(A, B), error>
fmt.scan3<A, B, C>(): Result<(A, B, C), error>
```

## os

```aril
// Process arguments. A slice view of os.Args (read-only convention).
os.args: []string

// Exit with a status code. Diverging — never returns; unifies with
// any expected type at the call site. See `language-spec.md`
// §Error handling for the rule that lets `os.exit` occupy a `match`
// arm or any other typed position.
os.exit(code: int)

// File I/O (suite uses just these two).
os.readFile(name: string):  Result<[]byte, error>     // Go: os.ReadFile
os.writeFile(name: string, data: []byte, perm: os.FileMode): Result<unit, error>

// Environment.
os.getenv(key: string): string                        // empty if unset
os.lookupEnv(key: string): Option<string>             // distinguishes unset

// Signal channel element (re-exported from os; matches Go's os.Signal interface).
type os.Signal = interface { signal(): unit; string(): string }

os.interrupt: os.Signal                               // SIGINT alias (Go: os.Interrupt)

// File handle (only the bits used by the suite).
class os.File implements io.Reader, io.Writer, io.Closer {
  // ... method set produced by bindgen
}
os.open(name: string):  Result<os.File, error>
os.create(name: string): Result<os.File, error>
os.stdin:  os.File
os.stdout: os.File
os.stderr: os.File
```

## io

```aril
// Reader / Writer / Closer — bound Go interfaces. Aril classes that
// `implements io.Reader` etc. integrate seamlessly with the rest of the
// binding (e.g. `io.readAll` accepts any class that implements Reader).
interface io.Reader  { read(p: []byte):  Result<int, error> }
interface io.Writer  { write(p: []byte): Result<int, error> }
interface io.Closer  { close():           Result<unit, error> }

// Composite interfaces used in net/http.
interface io.ReadCloser  extends io.Reader,  io.Closer
interface io.WriteCloser extends io.Writer,  io.Closer

// Slurp everything from a Reader (Go: io.ReadAll).
io.readAll(r: io.Reader): Result<[]byte, error>

// Stream copy.
io.copy(dst: io.Writer, src: io.Reader): Result<int64, error>

// Sentinel errors used by callers.
io.EOF: error
```

**Bound today:** `io.readAll(r): Result<[]byte, error>` (a mechanical `(T, error)`
row; `os.stdin` flows in as the reader). `io.copy`, `io.EOF`, and the
`Reader`/`Writer`/`Closer` interfaces as first-class values remain on the target
surface.

## strings

```aril
strings.fields(s: string): []string                   // splits on whitespace
strings.split(s: string, sep: string): []string
strings.join(parts: []string, sep: string): string
strings.trimSpace(s: string): string
strings.trimPrefix(s: string, p: string): string
strings.trimSuffix(s: string, suf: string): string
strings.trim(s: string, cutset: string): string
strings.trimLeft(s: string, cutset: string): string
strings.trimRight(s: string, cutset: string): string
strings.hasPrefix(s: string, p: string): bool
strings.hasSuffix(s: string, suf: string): bool
strings.contains(s: string, sub: string): bool
strings.count(s: string, sub: string): int
strings.replace(s: string, old: string, new: string, n: int): string
strings.toLower(s: string): string
strings.toUpper(s: string): string

// Bytes↔string conversion.
strings.fromBytes(b: []byte): string                  // Go: string(b)
strings.toBytes(s: string):   []byte                  // Go: []byte(s)
```

## strconv

```aril
// Integer / float parsing — Go-style (s, base, bitSize) maps to a Aril
// Result.
strconv.atoi(s: string):                                Result<int, error>
strconv.itoa(n: int):                                   string
strconv.parseInt(s: string, base: int, bitSize: int):   Result<int64, error>
strconv.parseFloat(s: string, bitSize: int):            Result<float64, error>
strconv.formatFloat(f: float64, fmt: rune, prec: int, bitSize: int): string
strconv.formatBool(b: bool): string
strconv.parseBool(s: string): Result<bool, error>
strconv.quote(s: string): string
```

## log

```aril
log.println(args: ...Any)
log.printf(format: string, args: ...Any)

// Fatal logs and then calls os.exit(1).
log.fatal(args: ...Any)
log.fatalf(format: string, args: ...Any)

// Configure the prefix; Go's log.SetPrefix.
log.setPrefix(prefix: string)
log.setFlags(flags: int)
```

**Bound today:** the full set above — `println` / `printf` / `print` / `fatal` /
`fatalf` / `setPrefix` / `setFlags`, as fire-and-forget effect renames (like
`fmt.print*`). `fatal*` log then exit in Go; a diverging-return annotation for
them is future surface.

## math/big

```aril
// Arbitrary-precision integer. Bound from Go's `*big.Int`; methods are
// the canonical ones the Timus / algorithmic suite needs. Aril hides
// Go's pointer-mutation calling convention (`x.Add(a, b)` mutating x)
// behind a functional surface — every method returns a fresh `BigInt`.
class big.BigInt {
  add(other: big.BigInt):   big.BigInt
  sub(other: big.BigInt):   big.BigInt
  mul(other: big.BigInt):   big.BigInt
  div(other: big.BigInt):   big.BigInt        // integer division
  mod(other: big.BigInt):   big.BigInt
  neg():                    big.BigInt
  cmp(other: big.BigInt):   int                // -1 / 0 / 1
  string():                 string
  toInt64():                int64              // truncates if out of range
}

big.fromInt(n: int):       big.BigInt
big.fromInt64(n: int64):   big.BigInt
big.fromString(s: string): Result<big.BigInt, error>
```

**Bound today:** `fromInt` / `fromInt64` and the `add` / `sub` / `mul` / `div`
(truncated) / `toInt64` method set — a runtime-backed value handle (the arilrt
`BigInt` wrapper, dual-mode). `mod` / `neg` / `cmp` / `string` / `fromString`
remain on the target surface.

## math

```aril
math.sqrt(x: float64): float64
math.abs(x: float64):  float64
math.pow(x: float64, y: float64): float64
math.log(x: float64):  float64                          // natural log
math.log10(x: float64): float64
math.log2(x: float64): float64
math.floor(x: float64): float64
math.ceil(x: float64): float64
math.min(a: float64, b: float64): float64
math.max(a: float64, b: float64): float64
math.pi: float64

// Integer min/max have no Go-stdlib referent (Go's math.{Min,Max} are
// float-typed). User code uses `if a < b { a } else { b }` for now;
// promoting them to Aril-side built-ins (cf. refEq) is park material.
```

## time

```aril
// Nominal newtype over int64 — wraps Go's time.Duration.
newtype time.Duration = int64

// Construction (matches Go's time.Second * N idiom, but via factory funcs).
time.seconds(n: int):      time.Duration
time.milliseconds(n: int): time.Duration

// Arithmetic on durations is method-based for v1 (operator overloading is
// not in scope for the v1 acceptance suite):
//   d1.add(d2: time.Duration): time.Duration
//   d.mul(n: int): time.Duration
//   d.string(): string                                    // Go's Duration.String

// Periodic ticks.
class time.Ticker {
  let c: RecvChan<time.Time>      // fires on each tick
  stop()
}
time.tick(d: time.Duration):    RecvChan<time.Time>      // simple ticker
time.newTicker(d: time.Duration): time.Ticker            // stoppable

// One-shot timeout signal.
time.after(d: time.Duration): RecvChan<time.Time>

// Sleep blocks the current goroutine.
time.sleep(d: time.Duration)
```

**Bound today:** the `time.seconds` / `time.milliseconds` constructors and the
`time.Duration` arithmetic surface — `add` / `mul` (lowered to Go operators, as
`Duration` is a scalar) + `string` (Go's `Duration.String`). `time.after` /
`tick` bind their `RecvChan<time.Time>` returns. Wall-clock `time.Time`, the
`Ticker`, and `newTicker` remain on the target surface.

## context

```aril
interface context.Context {
  done(): RecvChan<unit>                              // closes when cancelled
  err(): Option<error>
  deadline(): Option<time.Time>
  value(key: Any): Option<Any>
}

context.background(): context.Context                 // Go: context.Background()

// Two-value returns -> tuple. The cancel() is idempotent; defer it.
context.withCancel(parent: context.Context):
    (context.Context, func())
context.withTimeout(parent: context.Context, d: time.Duration):
    (context.Context, func())
```

**Bound today:** `context.Context` as a value handle with `done(): RecvChan<unit>`
— enough to honour cancellation in a `select` (the nested-scope callee pattern).
`err` (needs nullable-error → Option) / `deadline` (needs `time.Time`) / `value`
and the `background` / `withCancel` / `withTimeout` constructors remain on the
target surface.

## encoding/json

```aril
// Round-trip parse/serialize over a Aril record. Generic over the target
// type. Implementation under the hood uses Go reflection, so structural
// records map directly to JSON objects with field-name == JSON-key (or
// a future @json("…") attribute resolves the override).

json.parse<T>(data: []byte):   Result<T, error>
json.serialize(v: Any):        Result<[]byte, error>
// Not in v1 — needs bounded generics (`W: io.Writer`).
json.serializeTo<W: io.Writer>(w: W, v: Any): Result<unit, error>

// "Pretty-printed" variant of serialize.
json.serializeIndent(v: Any, prefix: string, indent: string): Result<[]byte, error>
```

## net

The low-level socket layer — the networking *base*. A `net.Conn` is a
bidirectional byte stream (read / write / close); a `net.Listener` accepts
connections. Every application protocol (HTTP included) is built on top of this
base, so the socket layer alone lets a program implement any protocol by hand.
Conn / Listener / Addr are surfaced as value handles (D37): Go interfaces, so the
handle's Go type is the bare interface spelling (no pointer).

```aril
// A connection — a byte stream. `read`/`write` return the byte count in a
// Result (mirroring Go's (int, error)); `close` returns Result<unit, error>.
// net.Conn is io.Reader + io.Writer + io.Closer, so it flows into io.readAll etc.
class net.Conn {
  read(p: []byte):  Result<int, error>
  write(p: []byte): Result<int, error>
  close():          Result<unit, error>
}

// A listening socket.
class net.Listener {
  accept(): Result<net.Conn, error>       // block for the next connection
  close():  Result<unit, error>
  addr():   net.Addr                       // the bound address (e.g. after :0)
}

class net.Addr {
  string(): string                         // "host:port"
}

// Dial a connection / open a listener. `network` is "tcp", "tcp4", "udp", …;
// `address` is "host:port" ("127.0.0.1:0" lets the OS pick a free port).
net.dial(network: string, address: string):   Result<net.Conn, error>
net.listen(network: string, address: string): Result<net.Listener, error>
```

**Bound today:** `net.dial` / `net.listen` (mechanical `(T, error)` registry rows
whose handle-typed returns the deriver spells from the local-package interface),
plus the `net.Conn` / `net.Listener` / `net.Addr` handle method sets above. The
Result-returning methods (`read`/`write`/`accept`/`close`) are the first handle
methods lifted via `ResultOf` / `ResultUnit`. A `net.dialTCP` convenience,
deadlines/timeouts, and UDP (`net.PacketConn`) remain on the target surface.

## bufio

```aril
// Line-by-line reader over an io.Reader.
class bufio.Scanner {
  scan(): bool                                     // false on EOF or error
  text(): string                                   // most recent line, no trailing \n
  bytes(): []byte
  err(): Option<error>
}

bufio.newScanner(r: io.Reader): bufio.Scanner

// Buffered writer over an io.Writer.
class bufio.Writer {
  write(p: []byte): Result<int, error>
  writeString(s: string): Result<int, error>
  flush(): Result<unit, error>
}

bufio.newWriter(w: io.Writer): bufio.Writer
```

**Bound today:** `bufio.newScanner(r)` + the `scan` / `text` method set — an
external value handle; its reader is `os.stdin` (bound, → Go's `os.Stdin`). The
rest of `Scanner` (`bytes` / `err`) and all of `bufio.Writer` remain on the
target surface.

## net/url

```aril
class url.URL {
  let scheme:   string
  let host:     string                    // host:port if a port is present
  let path:     string                    // request path; "" if the URL has none
  let rawQuery: string
  let fragment: string

  string(): string                        // re-serialize
  query(): url.Values                     // parse rawQuery into a multi-map
}

class url.Values {
  get(key: string):    string
  values(key: string): []string
  set(key: string, v: string)
  add(key: string, v: string)
}

url.parse(rawURL: string): Result<url.URL, error>
```

**Bound today:** `import url` resolves (→ Go `net/url`). `url.parse(rawURL)` is a
mechanical `Result<url.URL, error>` row (Go's `url.Parse`). `url.URL` is a bound
value-handle with a **field axis** — `scheme`/`host`/`path`/`rawQuery`/`fragment`
lower to Go's `Scheme`/`Host`/`Path`/`RawQuery`/`Fragment` — plus a `string()`
method (→ Go's `(*url.URL).String`). `query()` / `url.Values` remain on the target
surface above.

## net/http

```aril
// Core types.

interface http.Handler {
  serveHTTP(w: http.ResponseWriter, r: http.Request)
}

interface http.ResponseWriter {
  header(): http.Header
  write(p: []byte): Result<int, error>
  writeHeader(status: int)
}

// Convenience used in healthcheck_server.
http.ResponseWriter.writeString(s: string): Result<int, error>

// Wrapped struct of Go's *http.Request.
class http.Request {
  let method: string
  let url:    url.URL              // non-nullable (always-non-nil pointer)
  let body:   io.ReadCloser        // non-nullable; http.NoBody when empty
  let header: http.Header
  // ... more as needed

  withContext(ctx: context.Context): http.Request   // returns a shallow copy
}

// HTTP header — multi-valued, case-insensitive keys. Modelled as a class
// (not a Map), so `.get(key)` is the single-string convenience that 99 %
// of callers want; full multi-value access is via `.values(key)`.
class http.Header {
  get(key: string):           string            // first value, or "" if absent
  values(key: string):        []string          // all values; empty slice if absent
  set(key: string, v: string)
  add(key: string, v: string)
  delete(key: string)
}

// Constructors and the default client.
http.newRequest(method: string, url: string, body: Option<io.Reader>):
    Result<http.Request, error>

http.get(url: string):      Result<http.Response, error>
http.do(req: http.Request): Result<http.Response, error>

class http.Response {
  let statusCode: int
  let header:     http.Header
  let body:       io.ReadCloser
  // ...
}

// Servers.
class http.Server {
  let addr:    string
  let handler: http.Handler
  // ... timeouts etc.

  serve(l: net.Listener):         Result<unit, error>   // drive on an existing listener
  listenAndServe():               Result<unit, error>   // carry-forward (binds its own listener)
  shutdown(ctx: context.Context): Result<unit, error>
}

http.listenAndServe(addr: string, handler: http.Handler): Result<unit, error>
// Serve on an existing listener (net.Listen) — stoppable by closing the listener.
http.serve(l: net.Listener, handler: http.Handler): Result<unit, error>

// Sentinel.
http.ErrServerClosed: error
http.isServerClosed(e: error): bool                  // sugar for errors.Is
```

**Bound today:** `import http` resolves (→ Go `net/http`). `http.Handler` is a
**bound interface** an Aril class implements — the checker verifies the class
structurally provides `serveHTTP(w: http.ResponseWriter, r: http.Request)` and
reports a mismatch in Aril coordinates (E0219); the class lowers so `serveHTTP`
becomes Go's exported `ServeHTTP` and the class value (`&HealthHandler{}`)
satisfies Go's `http.Handler` structurally. `http.ResponseWriter` is a value
handle with `write` / `writeString` / `writeHeader` (`writeString` lowers to
`Write([]byte(s))`, since ResponseWriter is an io.Writer); `http.Request` exposes
its `method` (→ Go `Method`) and `url` (→ Go **`URL`** — the first *divergent-name*
handle field, yielding a `url.URL` handle so `r.url.path` routes; `url_router`
proves it) fields via the field axis, the rest of its surface a carry-forward.
`http.listenAndServe(addr,
handler)` and `http.serve(listener, handler)` are bound (mechanical `Result<unit,
error>` rows). The free `http.listenAndServe` blocks; `http.serve` over a
`net.Listen` listener is stoppable by closing the listener — the `http_server`
example uses it for a run-checkable in-process server driven by a raw `net`
client (the runtime proof of the conformance machinery). **`http.Server` is a
bound constructable handle with init fields** — the first stdlib handle built
with a brace-literal *field* (`http.Server{ handler: h, addr: a }`, lowering to
`&http.Server{Handler: h, Addr: a}`), vs the fieldless `sync.Mutex{}`; its
`serve(l: net.Listener)` and `shutdown(ctx)` methods (Go's `(*http.Server).Serve`
/ `.Shutdown`, `ResultUnit`-wrapped) drive a **graceful** server: `healthcheck_server`
now installs the Aril handler on an `http.Server`, `serve`s it, and — after an
in-process client reads `ok` — `shutdown`s it cleanly, so it is **run_ok** (no
longer `no-run`). `http.Server.listenAndServe()` (the self-listening form) stays a
carry-forward. **`http.Response` is a bound value-handle with a field axis** — the
first handle to expose *fields*, not just methods: `resp.statusCode` (int),
`resp.status` (string), `resp.header` (http.Header), `resp.body` (io.ReadCloser,
drained via `io.readAll`) lower to Go's exported `StatusCode`/`Status`/`Header`/
`Body` struct fields. **`http.Header`** is a bound method-only handle
(`get`/`values`/`set`/`add`/`delete`; Aril `delete` → Go's `Del`). **`http.get(url)`**
is bound — a mechanical `Result<http.Response, error>` row (Go's `http.Get`); the
`http_client` example fetches an in-process `http.serve` server, reads
`resp.statusCode` through the field axis, and drains `resp.body` via `io.readAll`.
The other client entry points (`http.do`, which needs `http.Client`/
`DefaultClient.Do`; `http.newRequest`, whose `Option<io.Reader>` body is an idiom),
the `http.Server` handle with `shutdown` (for a run-checkable server), and
`net/url` remain on the target surface above.

## sync

```aril
class sync.Mutex {
  lock()
  unlock()
  tryLock(): bool
}

class sync.RWMutex {
  rLock();  rUnlock()
  lock();   unlock()
}

class sync.WaitGroup {
  add(delta: int)
  done()
  wait()
}

class sync.Once {
  do(f: func())
}
```

**Bound today:** `sync.Mutex` (`lock`/`unlock`/`tryLock`) and `sync.WaitGroup`
(`add`/`done`/`wait`) — constructable value handles built in place with
`sync.Mutex{}` / `sync.WaitGroup{}` (the general `pkg.Type{}` qualified-type
construction path). `sync.RWMutex` and `sync.Once` (its `do` takes a `func()`
argument, which the handle-method param surface does not type yet) remain on the
target surface.

## os/signal

```aril
signal.notify(c: chan<-os.Signal, signals: ...os.Signal)
signal.stop(c: chan<-os.Signal)

// Aliases for common signals (Go exposes some via os.* and some via syscall.*).
signal.interrupt: os.Signal       // SIGINT
signal.terminate: os.Signal       // SIGTERM
```

## sort

```aril
// Comparator-based sort that **returns a new slice** — the Aril wrapper
// inverts Go's in-place mutation to keep `let` semantics meaningful.
sort.sorted<T>(s: []T, less: (T, T) => bool): []T

// Convenience: sort by an `Ord`-comparable key extractor.
sort.sortedBy<T, K>(s: []T, key: (T) => K): []T

// In-place mutation variant (matches Go's sort.Slice, takes a `var` slice).
sort.slice<T>(s: []T, less: (int, int) => bool)
```

## slices

```aril
// Slice-level helpers over Go's `slices` package (Go ≥1.21). The
// value-returning ones rename to the real package (Go infers the element
// type). `reverse` returns a NEW slice — Aril inverts Go's in-place
// slices.Reverse to keep `let` semantics.
slices.max<T: Ordered>(s: []T):        T        // Go slices.Max (panics if empty)
slices.min<T: Ordered>(s: []T):        T        // Go slices.Min
slices.contains<T>(s: []T, v: T):      bool      // Go slices.Contains
slices.indexOf<T>(s: []T, v: T):       int       // Go slices.Index (-1 if absent)
slices.reverse<T>(s: []T):             []T       // new reversed copy
slices.dedup<T>(s: []T):               []T       // new slice, duplicates removed (first-occurrence order; T comparable)
```

## errors

```aril
errors.new(text: string): error                        // alias for the built-in
errors.is(err: error, target: error): bool
errors.as<T>(err: error): Option<T>                    // generic As
```

**Bound today:** the full set — `errors.new` / `errors.is` (renames), and
`errors.as<T>` (generic), whose Go `errors.As(err, &t)` pointer-out protocol
lifts into `Option<T>` via the `ErrorsAs` runtime helper. A user `class X
implements error` has its `error(): string` method (and calls on its values)
lowered to Go's `Error()`, so the struct satisfies Go's `error` interface and is
matchable by `errors.as` (the error→Error boundary, D14 footnote).

## regexp

A value-handle type surfaced through the builtin-module namespace: the
constructor returns an opaque `regexp.Regexp` and the method set dispatches on
that boundary type (mechanical — each maps 1:1 to a Go method). The
capture-group / submatch surface is deferred.

```aril
regexp.mustCompile(expr: string): regexp.Regexp   // panics on a bad pattern

class regexp.Regexp {
  matchString(s: string): bool
  findAll(s: string, n: int): []string            // Go FindAllString; n<0 = all
}
```

## What is **not** in v1

These bindings exist in Go but are out of scope until later:

- `database/sql` — the canonical hard binding (`*Rows`, nil, resource
  lifecycle, `context`). Tracked as an open problem.
- `reflect`, `unsafe`, `runtime`, `syscall` — unportable in principle;
  bindgen owns them, no Aril-side surface is planned.
- `crypto/*`, `compress/*`, `image/*`, `text/template`, `html/template` —
  out of scope until cold-start strategy decides.
