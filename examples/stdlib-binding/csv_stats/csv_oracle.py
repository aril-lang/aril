#!/usr/bin/env python3
"""Oracle for csv_stats: reads a CSV whose first row is headers and
remaining rows are integers; prints per-column sum/min/max."""
import sys

def main():
    path = sys.argv[1]
    with open(path) as f:
        lines = [l.rstrip("\n") for l in f if l.strip()]
    headers = lines[0].split(",")
    ncols = len(headers)
    sums = [0] * ncols
    mins = [None] * ncols
    maxs = [None] * ncols
    for line in lines[1:]:
        parts = line.split(",")
        for i, p in enumerate(parts):
            v = int(p.strip())
            sums[i] += v
            if mins[i] is None or v < mins[i]:
                mins[i] = v
            if maxs[i] is None or v > maxs[i]:
                maxs[i] = v
    for i, h in enumerate(headers):
        print(f"{h}: sum={sums[i]} min={mins[i]} max={maxs[i]}")

if __name__ == "__main__":
    main()
