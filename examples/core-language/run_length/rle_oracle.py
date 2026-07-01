#!/usr/bin/env python3
"""
Oracle for run-length encoding corpus example.
Reads the input string from sys.argv[1] (same as the Aril binary), prints:
  Line 1: RLE-encoded form
  Line 2: round-trip decoded form (must equal input)

Usage: python3 rle_oracle.py <input-string>

FRICTION note: the Aril binary uses os.args[1] because fmt.scan<string>()
reads whitespace-delimited tokens and cannot distinguish empty-string input
from EOF on an empty stdin. os.args is the standard multi-case idiom in this
corpus.
"""
import sys


def encode(s: str) -> str:
    if not s:
        return ""
    result = []
    count = 1
    for i in range(1, len(s)):
        if s[i] == s[i - 1]:
            count += 1
        else:
            result.append(str(count) + s[i - 1])
            count = 1
    result.append(str(count) + s[-1])
    return "".join(result)


def decode(s: str) -> str:
    if not s:
        return ""
    result = []
    num_str = ""
    for ch in s:
        if ch.isdigit():
            num_str += ch
        else:
            count = int(num_str) if num_str else 1
            result.append(ch * count)
            num_str = ""
    return "".join(result)


def main():
    if len(sys.argv) < 2:
        print("usage: rle_oracle.py <string>", file=sys.stderr)
        sys.exit(1)
    s = sys.argv[1]
    enc = encode(s)
    print(enc)
    print(decode(enc))


if __name__ == "__main__":
    main()
