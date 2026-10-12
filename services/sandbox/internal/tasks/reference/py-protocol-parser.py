from dataclasses import dataclass, field


@dataclass
class Command:
    """Команда протокола: имя + словарь аргументов."""
    name: str
    args: dict = field(default_factory=dict)


def parse_protocol(text: str):
    """Разбирает текст протокола в список Command."""
    out = []
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        cmd = parts[0]
        args = {}
        for tok in parts[1:]:
            if "=" in tok:
                k, v = tok.split("=", 1)
            else:
                k, v = tok, ""
            args[k] = v
        out.append(Command(cmd, args))
    return out
