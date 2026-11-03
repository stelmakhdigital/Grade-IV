# Образ python-задач sandbox (spec-20 / BUG-20261103-025): python:3.12-slim
# не содержит pytest, а docker-режим исполняет задачи банка через
# `python3 -m pytest -q` при --network=none — pip install на прогон
# невозможен, pytest должен быть запечён в образ. Версия запинена для
# детерминизма проб и задач.
FROM python:3.12-slim
RUN pip install --no-cache-dir pytest==8.3.4
