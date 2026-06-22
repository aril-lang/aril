" Vim syntax file for the Aril programming language.
" Language:    Aril (.aril)
" Source:      Derived from the canonical lexical surface in
"              lang-spec/keywords.md and lang-spec/grammar.ebnf (and the
"              contract vocabulary from docs/rfcs/0006 + 0007). Those
"              files are the contract; this highlighter mirrors them and
"              must be kept in sync on any keyword / operator / literal
"              change (same discipline as the generated lexer).
"
" Install: see editors/vim/README.md.

if exists("b:current_syntax")
  finish
endif

let s:cpo_save = &cpo
set cpo&vim

" Aril identifiers are ASCII Letter (Letter|Digit)*, where Letter
" includes '_' (grammar.ebnf §Ident). Keep '-' OUT of iskeyword so the
" hyphenated channel-contract clauses below match as distinct words.
syntax iskeyword @,48-57,_

" =====================================================================
" Comments  (grammar.ebnf §Whitespace and comments)
" Block comments are NOT nested: the first */ closes the comment.
" =====================================================================
syn keyword arilTodo            contained TODO FIXME XXX NOTE HACK
syn match   arilLineComment     "//.*$"            contains=arilTodo,@Spell
syn region  arilBlockComment    start="/\*" end="\*/" contains=arilTodo,@Spell

" =====================================================================
" Reserved keywords  (keywords.md §Reserved keywords)
" =====================================================================
syn keyword arilKeyword         import extern implements extends static
syn keyword arilStorageClass    func let const var type class interface
syn keyword arilConditional     if else match
syn keyword arilRepeat          for while in
syn keyword arilStatement       return break continue defer try
syn keyword arilConcurrent      spawn scope select
syn keyword arilReceiver        this

" Contextual keywords (keywords.md §Contextual keywords): keywords only in
" position, ordinary identifiers elsewhere. Highlighted globally for
" practicality — the positional nuance is a lexer concern.
syn keyword arilLabel           case default
syn keyword arilKeyword         impl

" =====================================================================
" Boolean / unit literals  (keywords.md §Type literals)
" =====================================================================
syn keyword arilBoolean         true false
syn keyword arilConstant        unit

" =====================================================================
" Predeclared (reserved) type names  (keywords.md §Built-in identifiers)
" =====================================================================
syn keyword arilType            bool int int8 int16 int32 int64
syn keyword arilType            uint uint8 uint16 uint32 uint64
syn keyword arilType            float32 float64 byte rune string
syn keyword arilType            Any Dynamic error
" Generic builtin types
syn keyword arilType            Result Option Map Set Stack
syn keyword arilType            Channel SendChan RecvChan

" Variant constructors
syn keyword arilConstructor     Ok Err Some None

" Predeclared functions (the conversion names int/float64/... double as
" types above, which is faithful to keywords.md: they are both).
syn keyword arilBuiltin         panic error refEq makeChannel makeSlice

" =====================================================================
" Contracts  (RFC-0006 value/state, RFC-0007 channel trace)
" The separable `contract`/`channel` block heads and the clause / predicate
" vocabulary. Contextual per spec; highlighted as a distinct group so a
" reader can spot the executable specification at a glance.
" =====================================================================
" Only the low-collision, genuinely contract-specific words are highlighted
" globally. The RFC-0007 clause words that are also ordinary identifiers or
" method names — `send`/`recv`/`close` (channel methods, D11), `result`,
" `before`/`after`/`every`/`more`/`than`/`role`/`signal`/`channel` — are
" deliberately LEFT ORDINARY: globally keywording them would light up real
" channel code (`.send`/`.close`) and common bindings (`let result = …`) as
" if they were contract clauses. Scoping the full vocab to inside `contract`/
" `channel` blocks (a `syn region`) is the proper fix and a future refinement.
syn keyword arilContractKeyword contract requires ensures invariant
syn keyword arilContractKeyword old implies loop forbid eventually fairness
" Hyphenated clause keywords need a match (the '-' is not a word char)
syn match   arilContractKeyword "\<closed-by\>"
syn match   arilContractKeyword "\<delivered-to-all\>"
syn match   arilContractKeyword "\<offered-to-all\>"
syn match   arilContractKeyword "\<no-starvation\>"
syn match   arilContractKeyword "\<drains-before-scope-exit\>"
syn match   arilContractKeyword "\<drains-before-return\>"

" =====================================================================
" Declaration names — the identifier introduced by a declaration keyword.
" =====================================================================
syn match   arilFunction        "\(\<func\s\+\)\@<=\h\w*"
syn match   arilTypeDef         "\(\<\(type\|class\|interface\)\s\+\)\@<=\h\w*"

" =====================================================================
" Numeric literals  (grammar.ebnf §Integer / Floating-point literals)
" Vim syntax priority is LAST-defined-wins (:h :syn-priority), so the float
" matches come AFTER the decimal-int match — otherwise `1.5` is eaten as int
" `1` + `.` + int `5`. Hex/oct/bin before decimal. `_` is a digit separator.
" =====================================================================
syn match   arilNumber          "\<0x\x\(\x\|_\)*\>"
syn match   arilNumber          "\<0o\o\(\o\|_\)*\>"
syn match   arilNumber          "\<0b[01]\([01]\|_\)*\>"
syn match   arilNumber          "\<\d\(\d\|_\)*\>"
syn match   arilFloat           "\<\d\(\d\|_\)*\.\d\(\d\|_\)*\([eE][+-]\?\d\(\d\|_\)*\)\?\>"
syn match   arilFloat           "\<\d\(\d\|_\)*[eE][+-]\?\d\(\d\|_\)*\>"

" =====================================================================
" String and rune literals  (grammar.ebnf §String and rune literals)
" No multi-line strings; no raw-CR/LF inside. Escape set is the v1 subset
" (no \b \f \v \a, no \U): \n \t \r \\ \" \' \0 \xNN \uNNNN.
" =====================================================================
syn match   arilEscape          contained "\\\([ntr\\\"'0]\|x\x\x\|u\x\x\x\x\)"
syn match   arilEscapeError     contained "\\[^ntr\\\"'0xu]"
syn region  arilString          start=+"+ skip=+\\"+ end=+"+ oneline
                              \ contains=arilEscape,arilEscapeError,@Spell
syn match   arilRune            "'\(\\\([ntr\\\"'0]\|x\x\x\|u\x\x\x\x\)\|[^'\\]\)'"
                              \ contains=arilEscape

" =====================================================================
" Operators and structural punctuation  (keywords.md §Operators)
" =====================================================================
syn match   arilOperator        "->\|=>\|\.\.=\|\.\.\.\|\.\.\|<-"
syn match   arilOperator        "==\|!=\|<=\|>=\|&&\|||"
syn match   arilOperator        "[-+*%!<>=]"
" `/` is an operator only when it does NOT open a `//` or `/*` comment.
" Without this guard the single-`/` match (defined after the comment rules,
" and Vim is last-defined-wins) would preempt the comment at its opener.
syn match   arilOperator        "/\%(/\|\*\)\@!"
" FFI attribute head: @go("...") (keywords.md §Contextual keywords)
syn match   arilAttribute       "@\h\w*"

" =====================================================================
" Highlight links — map to standard groups so colorschemes apply.
" =====================================================================
hi def link arilTodo            Todo
hi def link arilLineComment     Comment
hi def link arilBlockComment    Comment
hi def link arilKeyword         Keyword
hi def link arilStorageClass    StorageClass
hi def link arilConditional     Conditional
hi def link arilRepeat          Repeat
hi def link arilStatement       Statement
hi def link arilConcurrent      Statement
hi def link arilReceiver        Identifier
hi def link arilLabel           Label
hi def link arilBoolean         Boolean
hi def link arilConstant        Constant
hi def link arilType            Type
hi def link arilConstructor     Constant
hi def link arilBuiltin         Function
hi def link arilContractKeyword PreProc
hi def link arilFunction        Function
hi def link arilTypeDef         Type
hi def link arilFloat           Float
hi def link arilNumber          Number
hi def link arilString          String
hi def link arilRune            Character
hi def link arilEscape          SpecialChar
hi def link arilEscapeError     Error
hi def link arilOperator        Operator
hi def link arilAttribute       PreProc

let b:current_syntax = "aril"

let &cpo = s:cpo_save
unlet s:cpo_save
