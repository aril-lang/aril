#!/usr/bin/env python3
"""Caesar cipher oracle: shift each ASCII letter forward by K (wrapping
within its case A-Z / a-z), leave non-letters unchanged.
Usage: caesar_oracle.py <shift> <text>
"""
import sys


def caesar(shift: int, text: str) -> str:
    out = []
    for ch in text:
        if 'A' <= ch <= 'Z':
            out.append(chr((ord(ch) - ord('A') + shift) % 26 + ord('A')))
        elif 'a' <= ch <= 'z':
            out.append(chr((ord(ch) - ord('a') + shift) % 26 + ord('a')))
        else:
            out.append(ch)
    return ''.join(out)


if __name__ == '__main__':
    shift = int(sys.argv[1])
    text = sys.argv[2]
    print(caesar(shift, text))
