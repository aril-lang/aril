# tests/ — compiler and binding test suites

Test layers are defined in `docs/architecture.md` section 6:

- L0  signature bugs eliminated by construction (generated bindings)
- L1  round-trip compilation (Aril -> Go -> `go build`)
- L2  structural diff of bindings against `go/types`
- L3  behavioral / differential testing on fuzzed inputs
- L4  Go `Example*` functions as oracles
- L5  `go vet` and `go test -race` on generated code

Generated Go must always pass `go build`, `go vet`, and — for concurrent
code — `go test -race`. The example programs in `examples/README.md` double as
end-to-end acceptance tests.
