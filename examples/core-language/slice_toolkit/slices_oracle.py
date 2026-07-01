#!/usr/bin/env python3
"""Oracle for slice_toolkit: mirrors slice_toolkit.aril output exactly.

Usage: python3 slices_oracle.py "5 3 8 3 1 8 5"
"""
import sys

def main():
    if len(sys.argv) != 2:
        print("usage: slices_oracle.py <space-separated-ints>", file=sys.stderr)
        sys.exit(2)

    raw = sys.argv[1].split(" ")
    nums = [int(x) for x in raw if x]

    if not nums:
        print("empty input")
        sys.exit(1)

    sorted_nums  = sorted(nums)
    unique_nums  = sorted(set(nums))
    reversed_nums = list(reversed(nums))
    max_val = max(nums)
    min_val = min(nums)

    def join(xs):
        return " ".join(str(x) for x in xs)

    print("sorted:", join(sorted_nums))
    print("unique:", join(unique_nums))
    print("reversed:", join(reversed_nums))
    print("max:", max_val)
    print("min:", min_val)

if __name__ == "__main__":
    main()
