def make_retry(attempts, backoff=0.0, sleep=None):
    """Фабрика декоратора retry с backoff (sleep иньектируется)."""
    if attempts < 1:
        attempts = 1

    def deco(fn):
        def wrapper(*a, **k):
            last = None
            for attempt in range(1, attempts + 1):
                try:
                    return fn(*a, **k)
                except Exception as e:
                    last = e
                    if attempt < attempts and sleep is not None:
                        sleep(backoff * attempt)
            raise last
        return wrapper
    return deco
