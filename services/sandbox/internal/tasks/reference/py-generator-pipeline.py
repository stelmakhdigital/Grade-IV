def gen_map(fn, it):
    """Ленивое применение fn к каждому элементу it."""
    for x in it:
        yield fn(x)


def gen_filter(pred, it):
    """Ленивый фильтр: оставляет x, где pred(x) True."""
    for x in it:
        if pred(x):
            yield x


def pipeline(source, *stages):
    """Лениво применяет stages (функции итератор -> итератор) к source слева направо."""
    it = source
    for st in stages:
        it = st(it)
    return it
