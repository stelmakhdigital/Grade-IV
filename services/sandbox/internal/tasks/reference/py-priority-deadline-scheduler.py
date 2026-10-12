def schedule_next(tasks, now):
    """Возвращает порядок выполнения задач (список id).

    Правила: просроченные (deadline < now) — вперёд; внутри группы — по
    приоритету (больше — раньше); равенство — по ближайшему дедлайну;
    полное равенство — по id.
    """
    def key(t):
        overdue = 0 if t["deadline"] < now else 1
        return (overdue, -t["priority"], t["deadline"], t["id"])

    return [t["id"] for t in sorted(tasks, key=key)]
