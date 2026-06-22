" Highlight-regression assertions for syntax/aril.vim.
"
" Sourced by run.sh after the fixture is loaded with the syntax active. Each
" check finds the first occurrence of a pattern and asserts the syntax group
" at that position (plus an optional column offset). This catches the whole
" *class* of Vim last-defined-wins priority bugs (a later match silently
" preempting an earlier one) deterministically — the bug class that let the
" `/` operator eat `//`/`/*` comments and (before reorder) ints eat floats.
"
" Writes a report to $ARIL_HL_OUT and exits non-zero (cquit) on any failure.

function! s:GroupOfFirst(pat, ...) abort
  call cursor(1, 1)
  let l:off = a:0 ? a:1 : 0
  let l:pos = searchpos(a:pat, 'cW')
  if l:pos == [0, 0]
    return 'NOTFOUND'
  endif
  return synIDattr(synID(l:pos[0], l:pos[1] + l:off, 1), 'name')
endfunction

" [ search pattern, column offset, expected highlight group ]
let s:checks = [
  \ ['//',           0, 'arilLineComment'],
  \ ['/\*',          0, 'arilBlockComment'],
  \ ['\<42\>',       0, 'arilNumber'],
  \ ['0xFF',         0, 'arilNumber'],
  \ ['3\.14',        0, 'arilFloat'],
  \ ['"',            0, 'arilString'],
  \ ['\\n',          0, 'arilEscape'],
  \ ["'a'",          0, 'arilRune'],
  \ ['\<func\>',     0, 'arilStorageClass'],
  \ ['myFunc',       0, 'arilFunction'],
  \ ['MyType',       0, 'arilTypeDef'],
  \ ['\<true\>',     0, 'arilBoolean'],
  \ ['\<Option\>',   0, 'arilType'],
  \ ['\<None\>',     0, 'arilConstructor'],
  \ ['\<panic\>',    0, 'arilBuiltin'],
  \ ['1 / 2',        2, 'arilOperator'],
  \ ['@go',          0, 'arilAttribute'],
  \ ['\<contract\>', 0, 'arilContractKeyword'],
  \ ['\<requires\>', 0, 'arilContractKeyword'],
  \ ]

let s:fails = []
for s:c in s:checks
  let s:got = s:GroupOfFirst(s:c[0], s:c[1])
  if s:got !=# s:c[2]
    call add(s:fails, printf('FAIL  /%s/  expected %s, got %s',
          \ s:c[0], s:c[2], empty(s:got) ? '(none)' : s:got))
  endif
endfor

if empty(s:fails)
  call writefile(['PASS: all ' . len(s:checks) . ' highlight checks ok'], $ARIL_HL_OUT)
  qall!
else
  call writefile(s:fails, $ARIL_HL_OUT)
  cquit 1
endif
