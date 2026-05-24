# std/ — generated standard-library bindings

Tide bindings for Go standard-library packages, produced by `internal/bindgen`
(see `docs/architecture.md` section 3).

These files are **generated**. Raw binding signatures are derived mechanically
from `go/packages` type information (decision D6 in `AI.md`); only the
idiomatic wrapper layer involves human/agent judgment. Do not hand-edit
signatures here.

Empty until Phase 3 (see `backlog.md`). First targets: `fmt`, `os`, `io`,
`errors`, `strconv`, `strings`, `bytes`, `time`, `context`, `encoding/json`.
