#!/usr/bin/env python3
"""Oracle for the Hailstone (Collatz) sequence.
Usage: python3 hailstone_oracle.py <n>
Prints two lines: the number of terms (including n and the final 1), then the
peak (maximum value reached in the sequence).
"""
import sys

def hailstone(n):
    seq = [n]
    while n != 1:
        if n % 2 == 0:
            n = n // 2
        else:
            n = 3 * n + 1
        seq.append(n)
    return seq

def main():
    n = int(sys.argv[1])
    seq = hailstone(n)
    print(len(seq))
    print(max(seq))

if __name__ == "__main__":
    main()
