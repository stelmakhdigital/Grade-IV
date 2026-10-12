from urllib.parse import unquote_plus


def parse_query(qs: str) -> dict:
    """Разбирает строку запроса URL в словарь (значения декодируются)."""
    out = {}
    if not qs:
        return out
    for pair in qs.split("&"):
        if not pair:
            continue
        if "=" in pair:
            key, val = pair.split("=", 1)
        else:
            key, val = pair, ""
        out[unquote_plus(key)] = unquote_plus(val)
    return out
