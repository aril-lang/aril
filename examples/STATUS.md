# Example conformance status

Generated from `examples/auto-status.json` by the `corpus-status` tool (`tools/corpus-status`) — do not edit by hand. Build and run the tool to refresh.

Three tracked metrics, each with a CI-enforced floor in `metric-floors.toml`:

- **build_ok — 76 / 84 examples build end-to-end** (floor 76).
- **diag_ok — 91 / 114 negative cases produce their expected diagnostic** (floor 79).
- **run_ok — 76 / 83 run-pass examples build and run as specified** (floor 76; behavioural: exit code, stdout vs an `expected_output` sidecar (exact) or `expected_patterns` (ordered subsequence) when present, no `forbidden_patterns` line present, and — built under `--contracts=panic` — every stated contract held; `no-run` examples excluded).

| Stage reached | Count |
|---|---|
| ✅ build (full pipeline) | 76 |
| emit / codegen fail | 5 |
| sema fail | 1 |
| parse fail | 2 |

## Per-example

| Example | Stage | First blocker |
|---|---|---|
| `examples/concurrency/concurrency/concurrency.aril` | build | — |
| `examples/concurrency/graceful_server/graceful_server.aril` | build | — |
| `examples/concurrency/parallel_fetcher/parallel_fetcher.aril` | build | — |
| `examples/concurrency/parallel_sum/parallel_sum.aril` | build | — |
| `examples/concurrency/pipeline/pipeline.aril` | build | — |
| `examples/concurrency/rate_limited/rate_limited.aril` | build | — |
| `examples/concurrency/select_showcase/select_showcase.aril` | build | — |
| `examples/concurrency/worker_pool/worker_pool.aril` | build | — |
| `examples/core-language/balanced_brackets/balanced_brackets.aril` | build | — |
| `examples/core-language/caesar_cipher/caesar_cipher.aril` | build | — |
| `examples/core-language/d01/d01.aril` | build | — |
| `examples/core-language/d02/d02.aril` | build | — |
| `examples/core-language/d03/d03.aril` | build | — |
| `examples/core-language/d04/d04.aril` | build | — |
| `examples/core-language/d05/d05.aril` | build | — |
| `examples/core-language/d07/d07.aril` | build | — |
| `examples/core-language/d08/d08.aril` | build | — |
| `examples/core-language/d09/d09.aril` | build | — |
| `examples/core-language/d11/d11.aril` | build | — |
| `examples/core-language/deep_destructure/deep_destructure.aril` | build | — |
| `examples/core-language/defer_demo/defer_demo.aril` | build | — |
| `examples/core-language/fizzbuzz/fizzbuzz.aril` | build | — |
| `examples/core-language/grade_classifier/grade_classifier.aril` | build | — |
| `examples/core-language/hailstone/hailstone.aril` | build | — |
| `examples/core-language/hello/hello.aril` | build | — |
| `examples/core-language/interfaces/interfaces.aril` | build | — |
| `examples/core-language/invert_binary_tree/invert_binary_tree.aril` | build | — |
| `examples/core-language/leaderboard/leaderboard.aril` | build | — |
| `examples/core-language/leetcode_3131/leetcode_3131.aril` | build | — |
| `examples/core-language/match_on_tuples/match_on_tuples.aril` | build | — |
| `examples/core-language/merge_intervals/merge_intervals.aril` | build | — |
| `examples/core-language/p1033/p1033.aril` | build | — |
| `examples/core-language/p1133/p1133.aril` | build | — |
| `examples/core-language/p1242/p1242.aril` | build | — |
| `examples/core-language/p1335/p1335.aril` | build | — |
| `examples/core-language/p1349/p1349.aril` | build | — |
| `examples/core-language/p1404/p1404.aril` | build | — |
| `examples/core-language/p1423/p1423.aril` | build | — |
| `examples/core-language/p1605/p1605.aril` | build | — |
| `examples/core-language/p1683/p1683.aril` | build | — |
| `examples/core-language/p1786/p1786.aril` | build | — |
| `examples/core-language/p1820/p1820.aril` | build | — |
| `examples/core-language/receipt_formatter/receipt_formatter.aril` | build | — |
| `examples/core-language/reverse_linked_list/reverse_linked_list.aril` | build | — |
| `examples/core-language/run_length/run_length.aril` | build | — |
| `examples/core-language/set_algebra/set_algebra.aril` | build | — |
| `examples/core-language/sieve/sieve.aril` | build | — |
| `examples/core-language/slice_toolkit/slice_toolkit.aril` | build | — |
| `examples/core-language/trebuchet/trebuchet.aril` | build | — |
| `examples/core-language/two_sum/two_sum.aril` | build | — |
| `examples/core-language/valid_parentheses/valid_parentheses.aril` | build | — |
| `examples/core-language/word_frequency/word_frequency.aril` | build | — |
| `examples/ffi/config_reader/config_reader.aril` | build | — |
| `examples/ffi/sum_numbers/sum_numbers.aril` | build | — |
| `examples/modeling-errors/error_chain/error_chain.aril` | build | — |
| `examples/modeling-errors/error_wrapping/error_wrapping.aril` | build | — |
| `examples/modeling-errors/errors_as_types/errors_as_types.aril` | build | — |
| `examples/modeling-errors/parse_int/parse_int.aril` | build | — |
| `examples/modeling-errors/rpn_calculator/rpn_calculator.aril` | build | — |
| `examples/modeling-errors/safe_divide/safe_divide.aril` | build | — |
| `examples/modeling-errors/vending_machine/vending_machine.aril` | build | — |
| `examples/stdlib-binding/char_histogram/char_histogram.aril` | build | — |
| `examples/stdlib-binding/config_loader/config_loader.aril` | build | — |
| `examples/stdlib-binding/counterstack/pentix_agent.aril` | build | — |
| `examples/stdlib-binding/csv_stats/csv_stats.aril` | build | — |
| `examples/stdlib-binding/duration_budget/duration_budget.aril` | build | — |
| `examples/stdlib-binding/env_config/env_config.aril` | build | — |
| `examples/stdlib-binding/line_numberer/line_numberer.aril` | build | — |
| `examples/stdlib-binding/option_defaults/option_defaults.aril` | build | — |
| `examples/stdlib-binding/reading_validator/reading_validator.aril` | build | — |
| `examples/stdlib-binding/regexp_extract/regexp_extract.aril` | build | — |
| `examples/stdlib-binding/service_config/service_config.aril` | build | — |
| `examples/stdlib-binding/statistics/statistics.aril` | build | — |
| `examples/stdlib-binding/struct_dump/struct_dump.aril` | build | — |
| `examples/stdlib-binding/todo_api/todo_api.aril` | build | — |
| `examples/stdlib-binding/wc/wc.aril` | build | — |
| `examples/concurrency/mutex_counter/mutex_counter.aril` | emit | go build failed |
| `examples/concurrency/nested_scopes/nested_scopes.aril` | emit | go build failed |
| `examples/modeling-errors/errors_as/errors_as.aril` | emit | go build failed |
| `examples/stdlib-binding/leveled_log/leveled_log.aril` | emit | go build failed |
| `examples/stdlib-binding/stdin_bytes/stdin_bytes.aril` | emit | go build failed |
| `examples/stdlib-binding/healthcheck_server/healthcheck_server.aril` | sema | error[E0103]: Unknown name http |
| `examples/concurrency/pubsub/pubsub.aril` | parse | error[E0112]: mixed brace-literal entry kinds |
| `examples/core-language/lru_cache/lru_cache.aril` | parse | error[E0112]: expected expression, got Punct "," |

## Diagnostic-quality gaps

Negative cases whose `.expected` records the **ideal** user-facing diagnostic that the compiler does not yet emit (e.g. a parser message still leaking internal token-kind names). This is the backlog the `diag_ok` metric grows toward; closing a row means improving the diagnostic, not the test.

**23 of 114 cases fall short of the ideal.**

| Case | Ideal (`.expected`) | Actual |
|---|---|---|
| `examples/core-language/deep_destructure/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "cause" |
| `examples/core-language/fizzbuzz/errors/missing-brace.patch` | error[E0112]: expected `{` | error[E0112]: expected Punct "{", got Newline "" |
| `examples/core-language/fizzbuzz/errors/missing-in.patch` | error[E0112]: expected `in` | error[E0112]: expected Keyword "in", got IntLit "1" |
| `examples/core-language/invert_binary_tree/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "right" |
| `examples/core-language/merge_intervals/errors/organic-merge3-e0112.patch` | error[E0112]: expected a type | error[E0112]: expected type expression, got Newline "" |
| `examples/core-language/p1242/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "v" |
| `examples/core-language/p1335/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "e" |
| `examples/core-language/p1349/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "e" |
| `examples/core-language/p1404/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "e" |
| `examples/core-language/p1605/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "e" |
| `examples/core-language/p1683/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "e" |
| `examples/core-language/p1786/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "b" |
| `examples/core-language/p1820/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "b" |
| `examples/core-language/set_algebra/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "b" |
| `examples/core-language/two_sum/errors/missing-comma-args.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "i" |
| `examples/core-language/two_sum/errors/missing-in-for.patch` | error[E0112]: expected `in` | error[E0112]: expected Keyword "in", got Ident "nums" |
| `examples/modeling-errors/error_chain/errors/organic-dividend2-e0112.patch` | error[E0112]: expected an expression | error[E0112]: expected expression, got Punct "." |
| `examples/modeling-errors/parse_int/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "v" |
| `examples/modeling-errors/rpn_calculator/errors/organic-parseop2-e0112.patch` | error[E0112]: expected `(` | error[E0112]: expected Punct "(", got Punct "," |
| `examples/modeling-errors/safe_divide/errors/bare-record-literal.patch` | error[E0112]: expected an expression | error[E0112]: expected expression, got Punct ":" |
| `examples/modeling-errors/vending_machine/errors/organic-step2-e0112.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Punct ":" |
| `examples/stdlib-binding/config_loader/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got Ident "cfg" |
| `examples/stdlib-binding/wc/errors/missing-comma.patch` | error[E0112]: expected `)` | error[E0112]: expected Punct ")", got StringLit "\"\\n\"" |

## Run failures

Run-pass examples that do not yet reach run_ok — they fail to build (an existing build_ok gap), exit non-zero (often awaiting argv/stdin), or time out. Closing a row means making the example run, not relaxing the check.

**7 of 83 run-pass examples fall short of run_ok.**

| Example | Status | Exit |
|---|---|---|
| `examples/concurrency/mutex_counter` | build-fail | 1 |
| `examples/concurrency/nested_scopes` | build-fail | 1 |
| `examples/concurrency/pubsub` | build-fail | 1 |
| `examples/core-language/lru_cache` | build-fail | 1 |
| `examples/modeling-errors/errors_as` | build-fail | 1 |
| `examples/stdlib-binding/leveled_log` | build-fail | 1 |
| `examples/stdlib-binding/stdin_bytes` | build-fail | 1 |
