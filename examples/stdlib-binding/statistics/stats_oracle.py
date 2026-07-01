#!/usr/bin/env python3
"""Oracle for statistics: reads space-separated numbers from argv[1],
prints mean/stddev(population)/min/max/median each to 4 decimal places."""
import sys
import math as _math

def main():
    if len(sys.argv) < 2:
        print("usage: stats_oracle.py '<numbers>'")
        sys.exit(2)
    nums = [float(x) for x in sys.argv[1].split()]
    n = len(nums)
    mean = sum(nums) / n
    # population std dev: sqrt(mean of squared deviations)
    variance = sum((x - mean) ** 2 for x in nums) / n
    stddev = _math.sqrt(variance)
    mn = min(nums)
    mx = max(nums)
    s = sorted(nums)
    if n % 2 == 0:
        median = (s[n // 2 - 1] + s[n // 2]) / 2.0
    else:
        median = s[n // 2]
    print(f"mean: {mean:.4f}")
    print(f"stddev: {stddev:.4f}")
    print(f"min: {mn:.4f}")
    print(f"max: {mx:.4f}")
    print(f"median: {median:.4f}")

if __name__ == "__main__":
    main()
