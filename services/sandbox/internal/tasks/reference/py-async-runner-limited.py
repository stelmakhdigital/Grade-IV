import asyncio


async def run_async(jobs, limit, retries=0, sleep=None):
    """Выполняет async-задачи jobs (список вызываемых) с лимитом concurrent
    (не более limit одновременно) и retries повторами при исключении.

    Возвращает список результатов в ПОРЯДКЕ jobs:
      - успех → возвращённое значение;
      - все попытки упали → последнее исключение (объект Exception).
    Между попытками одной задачи вызывается sleep(номер_попытки) — sync-функция
    или coroutine; если sleep=None — паузы нет. Реального сна не требуется.
    """
    if limit < 1:
        limit = 1
    sem = asyncio.Semaphore(limit)

    async def run_one(job):
        attempts = retries + 1
        async with sem:
            last = None
            for attempt in range(1, attempts + 1):
                try:
                    return await job()
                except Exception as e:
                    last = e
                    if attempt < attempts and sleep is not None:
                        r = sleep(attempt)
                        if asyncio.iscoroutine(r):
                            await r
            return last

    results = await asyncio.gather(*(run_one(j) for j in jobs))
    return list(results)
