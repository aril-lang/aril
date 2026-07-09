# Standard-library bindings — current status

What of the Go standard library is reachable from Aril **today**, as opposed to
what the binding surface eventually intends. The intended *target* spelling of
every call lives in [`binding-surface.md`](binding-surface.md) (a contract the
binding generator must eventually satisfy); this file is the honest snapshot of
what is actually wired through the compiler right now, so a reader opening the
project can tell at a glance whether a package they need is usable yet.

Snapshot: **v0.19.0** (2026-07-09).

## How a binding reaches Aril

A stdlib symbol is callable from Aril when it is part of the **builtin-module
surface** — an `import <pkg>` that the compiler recognises without an
`aril.toml`, lowering `pkg.method(...)` to the underlying Go call. There are two
ways a row gets there:

- **Mechanical** — the signature is derived from the Go type checker over a
  curated symbol list, with the standard wrapper rules (`(T, error)` →
  `Result<T, error>`, bare `error` → `Result<unit, error>`, `(T, bool)` →
  `Option<T>`, value/effect renames). One generated registry feeds both the type
  checker and code generation, so the two never drift.
- **Idiom** — a hand-authored wrapper that is *not* a mechanical signature
  transform: an effect that discards a Go `(int, error)` return, a runtime
  helper, or a Go type conversion. These are the deliberate carve-outs the
  mechanical deriver excludes.

Anything outside the builtin-module surface is **not yet available**: a program
importing it does not build. (Third-party Go packages reached through the
explicit `extern` FFI layer are a separate path — see [the FFI section of
`architecture.md`](architecture.md) and `aril import` — and are not covered
here.)

## Bound today — namespaces (free functions)

`import <name>` then call `<name>.method(...)`. The `Go path` column names the
package the import lowers onto (a rename means the Aril import name differs from
`path.Base`, e.g. `http` → `net/http`).

| Import | Go path | Bound symbols |
|---|---|---|
| `fmt` | `fmt` | `println` `print` `printf` (effects) · `sprint` `sprintf` `sprintln` · `errorf` (`%w` wrapping) · `scan` `scan2` `scan3` (stdin) |
| `os` | `os` | `args` `exit` `getenv` `readFile` `writeFile` · `lookupEnv` (→ `Option`) · `stdin` (a reader value) |
| `strings` | `strings` | `contains` `count` `fields` `hasPrefix` `hasSuffix` `index` `join` `replace` `split` `splitN` `toLower` `toUpper` `trim` `trimLeft` `trimPrefix` `trimRight` `trimSpace` `trimSuffix` · `fromBytes` `toBytes` |
| `strconv` | `strconv` | `atoi` `parseBool` `parseFloat` `parseInt` (→ `Result`) · `formatBool` `formatFloat` `itoa` `quote` |
| `math` | `math` | `abs` `ceil` `cos` `exp` `floor` `hypot` `log` `log10` `log2` `max` `min` `mod` `pi` `pow` `round` `sin` `sqrt` `tan` `trunc` (float64) |
| `unicode` | `unicode` | `isDigit` `isLetter` `isSpace` |
| `slices` | `slices` | `max` `min` `contains` `indexOf` · `reverse` `dedup` (new copy) |
| `sort` | `sort` | `sorted` (comparator) · `sortedBy` (key extractor) — return a new slice |
| `time` | `time` | `after` `tick` (→ `RecvChan<time.Time>`) · `sleep` · `seconds` `milliseconds` (`Duration` ctors) |
| `json` | `encoding/json` | `parse<T>` `serialize` `serializeIndent` (`Option` ⇄ `null` round-trip) |
| `errors` | `errors` | `is` (chain classification) · `new` (constructor) · `as<T>` (typed unwrap → `Option<T>`) |
| `io` | `io` | `readAll` (→ `Result<[]byte, error>`) |
| `log` | `log` | `println` `printf` `print` `fatal` `fatalf` `setPrefix` `setFlags` (effects) |
| `net` | `net` | `dial` `listen` (→ `Result`) — plus the `Conn` / `Listener` / `Addr` handles below |
| `http` | `net/http` | `get` `listenAndServe` `serve` (→ `Result`) — plus the handles + the `http.Handler` bound interface below |
| `url` | `net/url` | `parse` (→ `Result<url.URL, error>`) — plus the `URL` handle below |
| `sync` | `sync` | *(handle-only — `Mutex` / `WaitGroup` below)* |
| `context` | `context` | *(handle-only — `Context` below)* |
| `bufio` | `bufio` | *(handle-only — `Scanner` below)* |
| `regexp` | `regexp` | *(handle-only — `Regexp` below)* |
| `atomic` | `sync/atomic` | *(handle-only — `Int64` / `Uint64` / `Bool` + the generic `atomic.Pointer<T>` below)* |
| `reflect` | *(arilrt)* | `box` `unbox` `typeOf` `typeName` `kind` `fields` `fieldValue` `show` — a minimal reflection surface over the `Dynamic` handle, backed by the runtime (not a Go `reflect` mirror) |
| `big` | *(arilrt)* | *(handle-only — `BigInt` below, a functional wrapper over `math/big`)* |

Predeclared free functions `min` / `max` (generic over any ordered type — int,
float, string) fill the int-extremum gap `math.min` / `math.max` (float64-only)
leave; they lower to Go's builtin `min` / `max`.

## Bound today — value handles (types carrying methods)

A handle is a stdlib type surfaced with a fixed method set (and, for some, read
fields). A **constructable** handle is built with a brace literal
(`sync.Mutex{}`, `http.Server{ addr: ":0", handler: h }`); the others are
produced by a constructor call (`regexp.mustCompile(...)`, `url.parse(...)`) or
returned from a bound function (`net.dial(...)` → `net.Conn`).

| Handle | Constructable | Methods / fields |
|---|---|---|
| `regexp.Regexp` | no (`regexp.mustCompile`) | `matchString(string): bool` · `findAll(string, int): []string` |
| `bufio.Scanner` | no (`bufio.newScanner(io.Reader)`) | `scan(): bool` · `text(): string` |
| `big.BigInt` | no (`big.fromInt` / `fromInt64`) | `add` `sub` `mul` `div(big.BigInt): big.BigInt` · `toInt64(): int64` |
| `time.Duration` | no (`time.seconds` / `milliseconds`) | `add` `mul` (Go operators; `Duration` is a scalar) · `string(): string` |
| `sync.Mutex` | **yes** (`{}`) | `lock()` `unlock(): unit` · `tryLock(): bool` |
| `sync.WaitGroup` | **yes** (`{}`) | `add(int)` `done()` `wait(): unit` |
| `atomic.Int64` / `atomic.Uint64` | **yes** (`{}`) | `load()` · `store(v)` · `swap(v): old` · `compareAndSwap(old, new): bool` · `add(delta): new` |
| `atomic.Bool` | **yes** (`{}`) | `load(): bool` · `store(v)` · `swap(v): bool` · `compareAndSwap(old, new): bool` *(no `add`)* |
| `context.Context` | no (`nested_scopes` / scope wiring) | `done(): RecvChan<unit>` |
| `net.Conn` | no (`net.dial` / `Listener.accept`) | `read` `write([]byte): Result<int, error>` · `close(): Result<unit, error>` |
| `net.Listener` | no (`net.listen`) | `accept(): Result<net.Conn, error>` · `close(): Result<unit, error>` · `addr(): net.Addr` |
| `net.Addr` | no (`Listener.addr`) | `string(): string` |
| `http.Server` | **yes** (`{ handler, addr }`) | `serve(net.Listener): Result<unit, error>` · `shutdown(context.Context): Result<unit, error>` |
| `http.ResponseWriter` | no (handler arg) | `write` `writeString(...): Result<int, error>` · `writeHeader(int): unit` |
| `http.Request` | no (handler arg) | fields `method: string`, `url: url.URL` |
| `http.Response` | no (`http.get`) | fields `statusCode: int`, `status: string`, `header: http.Header`, `body` (an `io.ReadCloser`, drained via `io.readAll`) |
| `http.Header` | no (`Response.header`) | `get` `values` `set` `add` `delete` |
| `url.URL` | no (`url.parse`) | `string(): string` · fields `scheme` `host` `path` `rawQuery` `fragment` |

## Bound today — generic types

- **`atomic.Pointer<T>`** — a first-class generic type (modelled like `Map` /
  `Set`, not a handle-table row), the honest lock-free reference cell. Aril has
  no raw pointers: `T` is a class reference and "nil" is `Option`, so the cell
  loads an `Option<T>` (`None` = the nil pointer). Construct the empty cell with
  `atomic.Pointer<T>{}`. Method set: `load(): Option<T>` · `store(T): unit` ·
  `swap(Option<T>): Option<T>` · `compareAndSwap(Option<T>, Option<T>): bool`
  (CAS by reference identity, not structural). Lowers to an `arilrt` cell over
  Go's `atomic.Pointer[T]`; reclamation rides the garbage collector (GC-as-RCU),
  so there are no hazard pointers or `unsafe`.

(The language's own generic containers — `[]T`, `Map<K, V>`, `Set<T>`,
`Stack<T>`, the channel family — are built-ins, not stdlib bindings; see
[`language-spec.md`](language-spec.md) §Collections.)

## Bound today — interfaces an Aril class may implement

- **`http.Handler`** — a `class implements http.Handler` must provide
  `serveHTTP(http.ResponseWriter, http.Request): unit` (lowered to Go's
  `ServeHTTP`). Conformance is **structurally checked in sema**, in `.aril`
  coordinates (diagnostic **E0219**) — the first bound-interface conformance
  check, not a raw `go build` leak. `healthcheck_server` / `http_server`
  implement it.

This is currently the **only** enforced bound interface. (`error` is a language
built-in, not a stdlib binding; a class may `implements error` and the boundary
lowers its `error()` method to Go's `error` interface.)

## Not yet available

Grouped by how close each is to landing — the upper rows are small additive
bindings, the lower rows need translator work or are whole subsystems.

**Small surface gaps (a missing method on an already-bound package).**
The std streams `os.stdout` / `os.stderr` (`os.stdin` is bound — the reader
`bufio.newScanner` consumes). `sync.Once` / `sync.RWMutex` (the `scope` /
`spawn` / channel model plus `sync.Mutex` cover the current corpus). The wider
`time.Time` clock/formatting surface, `bufio.Writer`, and the wider `big`
surface (`mod` / `cmp`).

**Implementable Go interfaces beyond `http.Handler`.** `io.Reader` / `Writer` /
`Closer` and friends: a `net.Conn` *is* usable as these where a binding consumes
one (e.g. `bufio.newScanner`, `io.readAll`), but a class **authoring**
`implements io.Reader` is not yet method-set-checked or wired — only
`http.Handler` conformance is enforced today.

**Out of scope until a cold-start strategy decides.** `database/sql` (the
canonical hard binding — it also needs a third-party driver, an `extern`/module
path, not a stdlib binding), `os/signal`, `crypto/*`, `compress/*`, `image/*`,
`text/template`, `html/template`. `unsafe` / `runtime` / `syscall` are owned by
the binding generator with no Aril-side surface planned (`reflect` is the
exception — it carries the minimal runtime-backed surface listed above).
