# Aril syntax highlighting for Vim

Syntax highlighting for `.aril` source files, derived from the canonical
lexical surface (`lang-spec/keywords.md` and `lang-spec/grammar.ebnf`). It
covers the full v1 surface — keywords, predeclared types and constructors,
builtin functions, integer / float / string / rune literals (including the
`0x`/`0o`/`0b` and `_`-separator forms and the v1 escape subset), operators
and the FFI `@go` attribute — plus the **contract vocabulary** (RFC-0006
value/state and RFC-0007 channel trace), so the executable specification in
an example stands out at a glance.

## Install

### Native Vim / Neovim (no plugin manager)

Copy the two files into your runtime path, preserving the `syntax/` and
`ftdetect/` subdirectories:

```sh
# Vim
mkdir -p ~/.vim/syntax ~/.vim/ftdetect
cp editors/vim/syntax/aril.vim    ~/.vim/syntax/
cp editors/vim/ftdetect/aril.vim  ~/.vim/ftdetect/

# Neovim
mkdir -p ~/.config/nvim/syntax ~/.config/nvim/ftdetect
cp editors/vim/syntax/aril.vim    ~/.config/nvim/syntax/
cp editors/vim/ftdetect/aril.vim  ~/.config/nvim/ftdetect/
```

Symlinking instead of copying keeps the highlighter current as the repo
evolves:

```sh
ln -s "$PWD/editors/vim/syntax/aril.vim"   ~/.vim/syntax/aril.vim
ln -s "$PWD/editors/vim/ftdetect/aril.vim" ~/.vim/ftdetect/aril.vim
```

### Plugin managers

Point any path-based manager at `editors/vim` as a package directory, e.g.
with `vim-plug` against a local checkout:

```vim
Plug '~/path/to/aril/editors/vim'
```

or drop `editors/vim` under a `pack/*/start/` directory for Vim 8 native
packages.

## Verify

Open any example and confirm it colors:

```sh
vim examples/core-language/merge_intervals/merge_intervals.aril
```

`:set filetype?` should report `filetype=aril`.

## Test

`test/run.sh` is a highlight-regression test: it loads the syntax file in a
headless Vim against `test/highlight_fixture.aril` and asserts (via `synID`)
that each representative token — line/block comment, int/float/hex, string,
escape, rune, keyword, type, constructor, builtin, operator, `@go`
attribute, contract clause — highlights as the right group.

```sh
editors/vim/test/run.sh    # exit 0 = pass, 1 = a check failed, skips if vim absent
```

It guards the class of bugs a "loads without error" check cannot see: Vim's
*last-defined-wins* match priority silently letting a later rule preempt an
earlier one (e.g. the `/` operator eating `//`/`/*` comments, or an int match
eating a float). Add a check here whenever you add a syntax group.

## Keeping it in sync

The highlighter mirrors the spec; it is not generated. When a keyword,
operator, literal form, or contract clause changes in
`lang-spec/keywords.md` / `lang-spec/grammar.ebnf`, update `syntax/aril.vim`
in the same change — the same paired-edit discipline the lexer follows.
