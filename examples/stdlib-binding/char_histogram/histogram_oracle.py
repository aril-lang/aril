#!/usr/bin/env python3
"""
histogram_oracle.py — reference implementation for char_histogram.

Usage: python3 histogram_oracle.py <string>

Classifies each character using Python's str.isalpha / isdigit / isspace
(which are Unicode-aware), then prints four lines matching the Aril program's
output format.

Note: the Aril fallback uses ASCII-range checks, so it matches this oracle
for all ASCII input but would diverge for non-ASCII letters/digits (e.g.
accented characters like 'é' are "other" in Aril but "letters" here).
"""
import sys

def histogram(s: str) -> dict:
    letters = sum(1 for c in s if c.isalpha())
    digits  = sum(1 for c in s if c.isdigit())
    spaces  = sum(1 for c in s if c.isspace())
    other   = sum(1 for c in s if not c.isalpha() and not c.isdigit() and not c.isspace())
    return {"letters": letters, "digits": digits, "spaces": spaces, "other": other}

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: histogram_oracle.py <string>", file=sys.stderr)
        sys.exit(2)
    h = histogram(sys.argv[1])
    print(f"letters: {h['letters']}")
    print(f"digits: {h['digits']}")
    print(f"spaces: {h['spaces']}")
    print(f"other: {h['other']}")
