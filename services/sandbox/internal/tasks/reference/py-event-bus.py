class EventBus:
    """Мини event-bus: subscribe / unsubscribe / emit."""

    def __init__(self):
        self._subs = {}

    def subscribe(self, event, handler):
        self._subs.setdefault(event, []).append(handler)
        return handler

    def unsubscribe(self, event, handler):
        lst = self._subs.get(event, [])
        if handler in lst:
            lst.remove(handler)

    def emit(self, event, *args, **kwargs):
        for h in list(self._subs.get(event, [])):
            h(*args, **kwargs)
