#!/usr/bin/env python3
"""
Oracle for balanced-brackets: argv[1] -> "balanced" or "not balanced".
Supports (), [], {}. All three bracket kinds must be correctly nested and matched.
"""
import sys

def is_balanced(s: str) -> bool:
    openers = {'(', '[', '{'}
    match = {')': '(', ']': '[', '}': '{'}
    stack = []
    for c in s:
        if c in openers:
            stack.append(c)
        elif c in match:
            if not stack or stack[-1] != match[c]:
                return False
            stack.pop()
        # non-bracket characters are ignored
    return len(stack) == 0

if __name__ == '__main__':
    s = sys.argv[1] if len(sys.argv) > 1 else ''
    print('balanced' if is_balanced(s) else 'not balanced')
