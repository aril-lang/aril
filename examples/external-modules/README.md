# External modules

Examples that depend on **another Aril module** — the RFC-0008 dependency
system. A project declares its dependencies in `aril.toml` under
`[dependencies.<name>]`, and imports the module by its name.

## `greeter/`

Depends on a pure-Aril library (`kind = "aril"`). The dependency is declared
with a `replace` pointing at an in-repo copy of the module
(`testdata/aril-modules/greetlib/`) — the vendored/dev form of a dependency;
the fetch form pins `source` + `version` instead. The library's exported
functions (`Greeting`, `Shout`) are compiled into the build and called
directly — there is no separate Go module (Aril modules compose as source, one
Go module out).

The library lives under `testdata/` rather than `examples/` so the corpus tool
(which builds every `.aril` under `examples/` as a standalone program) does not
mistake a library module for a runnable example.
