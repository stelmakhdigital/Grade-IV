PAIRS = {')': '(', ']': '[', '}': '{'}


def is_valid(s: str) -> bool:
    """True, если скобки в s корректно закрыты."""
    stack = []
    for ch in s:
        if ch in "([{":
            stack.append(ch)
        elif ch in ")]}":
            if not stack or stack.pop() != PAIRS[ch]:
                return False
    return not stack
