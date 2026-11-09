# syntax=docker/dockerfile:1
FROM python:3.13-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.13.7 /uv /bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app

COPY pyproject.toml uv.lock README.md ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --locked --no-dev --no-install-project
COPY src ./src
RUN --mount=type=cache,target=/root/.cache/uv uv sync --locked --no-dev --no-editable

# Load the OCR models now so a running container never downloads anything.
RUN .venv/bin/python -c "from rapidocr import RapidOCR; RapidOCR()"


FROM python:3.13-slim
RUN useradd --uid 1654 --no-create-home --shell /usr/sbin/nologin app
WORKDIR /app
COPY --from=build --chown=app:app /app/.venv /app/.venv
# The release, reported with every trace, metric, and log (release.yml passes the tag's version)
ARG VERSION=dev
ENV PATH="/app/.venv/bin:$PATH" PYTHONUNBUFFERED=1 PARSER_VERSION=$VERSION
USER app
EXPOSE 50051
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["python", "-m", "finchie_parser.healthcheck"]
CMD ["python", "-m", "finchie_parser"]
