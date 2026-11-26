# grade-voice (Python; WP-4 — ML-стек faster-whisper/Silero, на AI-узле — GPU)
FROM python:3.12-slim
WORKDIR /app
# pypi на канале с частыми обрывами: дефолтные 15 с на чтение и 5 попыток
# не вытягивают установку gigaam (сборка из tar.gz тянет setuptools).
ENV PIP_DEFAULT_TIMEOUT=120 PIP_RETRIES=8
COPY requirements.txt ./
# torch/torchaudio — с CPU-индекса и ДО requirements.txt: иначе pip тянет
# nvidia-* (~2 ГБ) и образ раздувается (то же делают make install и CI-джоба
# voice; см. комментарий в services/voice/requirements.txt). GPU-узел —
# --build-arg TORCH_INDEX_URL=https://download.pytorch.org/whl/cu126.
ARG TORCH_INDEX_URL=https://download.pytorch.org/whl/cpu
RUN pip install --no-cache-dir torch torchaudio --index-url ${TORCH_INDEX_URL}
# pypi.org на части сетей не поднимает TLS (handshake timeout), и установка
# gigaam из tar.gz падает на выборе setuptools. Зеркало задают в .env
# (PIP_INDEX_URL=…): compose передаёт его в build.args, дефолт — pypi.org.
ARG PIP_INDEX_URL=https://pypi.org/simple
RUN pip install --no-cache-dir --index-url ${PIP_INDEX_URL} -r requirements.txt
COPY app ./app
# Пишем — ничего: /models примонтирован :ro, веса скачиваются скриптом до
# запуска. HOME нужен torch на кэш; root в контейнере не нужен вовсе.
RUN useradd -u 1000 -m -d /home/grade grade
USER grade
ENV HOME=/home/grade
EXPOSE 8100
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD python -c "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8100/api/v1/health',timeout=4)" || exit 1
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8100"]
