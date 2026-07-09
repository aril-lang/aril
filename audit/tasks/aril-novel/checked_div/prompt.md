# Task: checked integer division

Write a function that divides one integer by another and **reports division by
zero as a recoverable error instead of crashing** — the caller must be forced to
handle the failure, not discover it at runtime. Do not use a sentinel value
(like returning 0 or -1) to signal the error, and do not let the program abort.

Then, for each of these `(a, b)` pairs in order — `(10, 2)`, `(7, 0)`,
`(9, 3)` — call the function and print one line:

- on success: `ok ` followed by the quotient (integer division)
- on the zero-divisor failure: `err ` followed by the message `division by zero`

Print nothing else.

## Exact expected output

```
ok 5
err division by zero
ok 3
```
