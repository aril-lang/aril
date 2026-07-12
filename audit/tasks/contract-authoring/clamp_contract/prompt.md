# Task: clamp, with a contract

Write a function `clamp(x, lo, hi)` that returns `x` limited to the range
`[lo, hi]`: if `x` is below `lo` it returns `lo`, if above `hi` it returns `hi`,
otherwise `x` itself.

**Attach a contract** to the function that (a) requires the caller to pass
`lo <= hi`, and (b) guarantees the result is within `[lo, hi]`.

Then print `clamp(5, 0, 10)`, `clamp(-3, 0, 10)`, and `clamp(15, 0, 10)` — each
on its own line, in that order.

Print nothing else.

## Exact expected output

```
5
0
10
```
