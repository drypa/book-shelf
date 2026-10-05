# syntax=docker/dockerfile:1
# Требуется BuildKit (Docker 23+). Кэш-монти не влияют на корректность, только на скорость.

ARG GO_VERSION=1.24

# ---------- build ----------
FROM golang:${GO_VERSION}-bookworm AS build
ENV CGO_ENABLED=1 \
    GOOS=linux \
    CGO_CFLAGS="-O2 -g0"
WORKDIR /src

# Слой зависимостей отдельно от кода: правка .go не пересобирает модули.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY . .

# CGO_ENABLED=1 обязателен: mattn/go-sqlite3. -trimpath и -s -w уменьшают бинарь
# и убирают абсолютные пути сборки (воспроизводимость).
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    for b in bot scan db-create; do \
      go build -trimpath -ldflags="-s -w" -o "/out/$b" "./cmd/$b"; \
    done

# ---------- runtime base ----------
FROM debian:bookworm-slim AS runtime
# tzdata нужен всем (метки времени в логах), appuser — непривилегированный пользователь.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends tzdata; \
    rm -rf /var/lib/apt/lists/*; \
    groupadd --system --gid 10001 app; \
    useradd  --system --uid 10001 --gid 10001 --create-home --shell /usr/sbin/nologin app
ENV TZ=UTC
WORKDIR /

# ---------- bot ----------
# Единственный сетевой компонент: ему, и только ему, нужны CA-сертификаты.
FROM runtime AS bot
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/bot /usr/local/bin/bot
USER app:app
ENTRYPOINT ["/usr/local/bin/bot"]

# ---------- scan ----------
FROM runtime AS scan
COPY --from=build /out/scan /usr/local/bin/scan
USER app:app
ENTRYPOINT ["/usr/local/bin/scan"]

# ---------- db-create ----------
FROM runtime AS db-create
COPY --from=build /out/db-create /usr/local/bin/db-create
USER app:app
ENTRYPOINT ["/usr/local/bin/db-create"]