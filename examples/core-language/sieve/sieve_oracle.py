#!/usr/bin/env python3
"""Sieve of Eratosthenes oracle.

Usage: python3 sieve_oracle.py <N>
Prints all primes <= N, space-separated, with a trailing newline.
"""
import sys


def sieve(n: int) -> list[int]:
    if n < 2:
        return [2]
    composite = [False] * (n + 1)
    composite[0] = True
    composite[1] = True
    i = 2
    while i * i <= n:
        if not composite[i]:
            j = i * i
            while j <= n:
                composite[j] = True
                j += i
        i += 1
    return [k for k in range(2, n + 1) if not composite[k]]


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: sieve_oracle.py <N>", file=sys.stderr)
        sys.exit(1)
    n = int(sys.argv[1])
    primes = sieve(n)
    print(" ".join(str(p) for p in primes))
