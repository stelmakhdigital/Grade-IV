from collections import OrderedDict


def cache(ttl, clock, maxsize):
    """Декоратор TTL-кэша с LRU-вытеснением (см. docstring в starter'е)."""
    if maxsize < 1:
        maxsize = 1

    def deco(fn):
        entries = OrderedDict()  # ключ → (value, put_time)

        def make_key(args, kwargs):
            return (args, tuple(sorted(kwargs.items())))

        def wrapper(*args, **kwargs):
            key = make_key(args, kwargs)
            now = clock()
            if key in entries:
                value, put_time = entries[key]
                if now < put_time + ttl:
                    entries.move_to_end(key)
                    return value
                del entries[key]
            value = fn(*args, **kwargs)
            entries[key] = (value, now)
            entries.move_to_end(key)
            while len(entries) > maxsize:
                entries.popitem(last=False)
            return value

        def size():
            return len(entries)

        wrapper.cache_size = size
        return wrapper

    return deco
