# Binding surface (target sketch)

The intended Tide-side spelling of every Go-stdlib call and type used by the
v1 acceptance suite (`examples/`). This document is a **contract**: when
the binding generator (`internal/bindgen`) lands, the surface it produces
for these packages must match this sketch. The wrapper layer's design
decisions — `(T, error)` → `Result`, nullable-pointer → `Option`,
non-nullable pointer → direct, variadic `interface{}` → `...Any` — apply
throughout.

> Note. Names follow the convention from `docs/language-spec.md`:
> exported Go identifiers are exposed in their lowerCamel form
> (`fmt.Println` → `fmt.println`); types keep PascalCase
> (`http.Handler`, `http.Request`).

## fmt

```td
// Variadic output. Each argument widens to Any at the call site (G23).
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
```

## os

```td
// Process arguments. A slice view of os.Args (read-only convention).
os.args: []string

// Exit with a status code.
os.exit(code: int)

// File I/O (suite uses just these two).
os.readFile(name: string):  Result<[]byte, error>     // Go: os.ReadFile
os.writeFile(name: string, data: []byte, perm: os.FileMode): Result<unit, error>

// Environment.
os.getenv(key: string): string                        // empty if unset
os.lookupEnv(key: string): Option<string>             // distinguishes unset

// Signal channel element (re-exported from os; matches Go's os.Signal interface).
type os.Signal = interface { signal(): unit; string(): string }

os.Interrupt: os.Signal                               // SIGINT alias

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

```td
// Reader / Writer / Closer — bound Go interfaces. Tide classes that
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

## strings

```td
strings.fields(s: string): []string                   // splits on whitespace
strings.split(s: string, sep: string): []string
strings.join(parts: []string, sep: string): string
strings.trimSpace(s: string): string
strings.trimPrefix(s: string, p: string): string
strings.trimSuffix(s: string, suf: string): string
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

```td
// Integer / float parsing — Go-style (s, base, bitSize) maps to a Tide
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

```td
log.println(args: ...Any)
log.printf(format: string, args: ...Any)

// Fatal logs and then calls os.exit(1).
log.fatal(args: ...Any)
log.fatalf(format: string, args: ...Any)

// Configure the prefix; Go's log.SetPrefix.
log.setPrefix(prefix: string)
log.setFlags(flags: int)
```

## time

```td
// Nominal newtype — see D11 (G needed). Wraps Go's time.Duration int64.
newtype time.Duration

// Construction (matches Go's time.Second * N idiom, but via factory funcs).
time.nanoseconds(n: int):  time.Duration
time.microseconds(n: int): time.Duration
time.milliseconds(n: int): time.Duration
time.seconds(n: int):      time.Duration
time.minutes(n: int):      time.Duration
time.hours(n: int):        time.Duration

// Arithmetic on durations — overloads of +, -, *, / against int.
// Operator overloading is `open` — for v1 use methods if `+` is not yet
// available:
//   d1.add(d2): time.Duration
//   d.mul(n: int): time.Duration

// Time points.
class time.Time { /* opaque; methods produced by bindgen */ }
time.now(): time.Time
time.since(t: time.Time): time.Duration

// Sleep blocks the current goroutine.
time.sleep(d: time.Duration)
```

## context

```td
interface context.Context {
  done(): chanRecv<unit>                              // close-channel pattern
  err(): Option<error>
  deadline(): Option<time.Time>
  value(key: Any): Option<Any>
}

context.background(): context.Context                 // Go: context.Background()
context.todo():       context.Context                 // Go: context.TODO()

// Two-value returns -> tuple (G24). cancel() is idempotent; defer it.
context.withCancel(parent: context.Context):
    (context.Context, func())
context.withTimeout(parent: context.Context, d: time.Duration):
    (context.Context, func())
context.withDeadline(parent: context.Context, deadline: time.Time):
    (context.Context, func())

context.withValue(parent: context.Context, key: Any, value: Any):
    context.Context
```

## encoding/json

```td
// Round-trip parse/serialize over a Tide record. Generic over the target
// type. Implementation under the hood uses Go reflection, so structural
// records map directly to JSON objects with field-name == JSON-key (or
// a future @json("…") attribute resolves the override).

json.parse<T>(data: []byte):   Result<T, error>
json.serialize(v: Any):        Result<[]byte, error>
json.serializeTo<W: io.Writer>(w: W, v: Any): Result<unit, error>

// "Pretty-printed" variant of serialize.
json.serializeIndent(v: Any, prefix: string, indent: string): Result<[]byte, error>
```

## net/http

```td
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
  let url:    url.URL              // non-nullable per G34
  let body:   io.ReadCloser        // non-nullable per G34; http.NoBody when empty
  let header: http.Header
  // ... more as needed

  withContext(ctx: context.Context): http.Request   // returns a shallow copy
}

// Header is a Map<string, []string> with case-insensitive operations.
class http.Header {
  get(key: string): string
  set(key: string, value: string)
  add(key: string, value: string)
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

  listenAndServe():       Result<unit, error>
  shutdown(ctx: context.Context): Result<unit, error>
}

http.listenAndServe(addr: string, handler: http.Handler): Result<unit, error>

// Sentinel.
http.ErrServerClosed: error
http.isServerClosed(e: error): bool                  // sugar for errors.Is
```

## sync

```td
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

## os/signal

```td
signal.notify(c: chan<-os.Signal, signals: ...os.Signal)
signal.stop(c: chan<-os.Signal)

// Aliases for common signals (Go exposes some via os.* and some via syscall.*).
signal.interrupt: os.Signal       // SIGINT
signal.terminate: os.Signal       // SIGTERM
```

## sort

```td
// Comparator-based sort that **returns a new slice** — the Tide wrapper
// inverts Go's in-place mutation to keep `let` semantics meaningful.
sort.sorted<T>(s: []T, less: (T, T) => bool): []T

// Convenience: sort by an `Ord`-comparable key extractor.
sort.sortedBy<T, K>(s: []T, key: (T) => K): []T

// In-place mutation variant (matches Go's sort.Slice, takes a `var` slice).
sort.slice<T>(s: []T, less: (int, int) => bool)
```

## errors

```td
errors.new(text: string): error                        // alias for the built-in
errors.is(err: error, target: error): bool
errors.as<T>(err: error): Option<T>                    // generic As
```

## What is **not** in v1

These bindings exist in Go but are out of scope until later:

- `database/sql` — the canonical hard binding (`*Rows`, nil, resource
  lifecycle, `context`). Tracked as an open problem.
- `reflect`, `unsafe`, `runtime`, `syscall` — unportable in principle;
  bindgen owns them, no Tide-side surface is planned.
- `regexp` — useful but not forced by the v1 suite; will land when AoC
  / real-world examples demand it.
- `crypto/*`, `compress/*`, `image/*`, `text/template`, `html/template` —
  out of scope until cold-start strategy decides.
