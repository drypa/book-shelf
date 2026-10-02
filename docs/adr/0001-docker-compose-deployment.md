# ADR-0001. Развёртывание проекта book-shelf через Docker Compose

- **Статус:** Proposed (к реализации)
- **Дата:** 2026-10-02
- **Автор:** software-architect
- **Задача:** [`docs/tasks/docker-compose-deploy.md`](../tasks/docker-compose-deploy.md)
- **Затрагивает:** `cmd/bot`, `cmd/scan`, `cmd/db-create`, новый пакет `internal/config`, новый пакет `internal/logging`
- **Соглашение об именовании:** `docs/adr/NNNN-<краткое-название>.md`, последовательная нумерация

---

## 1. Контекст

Проект — личная библиотека электронных книг: Telegram-бот ищет FB2-книги, хранящиеся в
ZIP-архивах. Индексация офлайновая, тремя отдельными бинарями:

1. `cmd/scan` — обходит каталог с `*.zip`, распаковывает каждый в temp-каталог, парсит
   `*.fb2`, пишет sidecar `<archive>.zip.json` **рядом с исходным архивом** (то есть
   мутирует каталог библиотеки).
2. `cmd/db-create` — читает `*.zip.json` и делает `INSERT` в таблицу `books`.
3. `cmd/bot` — долгоживущий процесс, обслуживает поиск и отдачу файлов.

Сейчас всё запускается вручную бинарями, собранными локально (`go run` / `go build`),
конфигурация читается только из окружения процесса, логи пишутся redirect-ом в файлы
(`log.log`, `scan-books.log` — оба файла присутствуют в рабочем каталоге репозитория).

### 1.1. Фактическое состояние конфигурации (проверено по коду)

| Переменная | Кто читает | Смысл | Проблема |
|---|---|---|---|
| `BOT_TOKEN` | `cmd/bot/main.go:24` | токен Telegram-бота | обязательная, но при отсутствии — тихий `return` с кодом 0 |
| `DB_PATH` | `cmd/bot/main.go:18` | **каталог с `.zip`-архивами** | имя вводит в заблуждение: это не путь к БД |
| `DB_CONNECTION_STRING` | `cmd/bot/main.go:41` | DSN для `sql.Open("sqlite3", …)` | имя неудобное; проверяется **после** `newStorage()` |
| `HTTP_PROXY` | `cmd/bot/main.go:30` | прокси к `api.telegram.org` | необязательная, но пустое значение не отсекается на уровне конфигурации |
| `SCAN_PARALLELISM` | `cmd/scan/main.go:10` | параллелизм распаковки | необязательная, дефолт 5 |

Дополнительные детали, важные для проектирования:

- Чтение переменных идёт через `viper.AutomaticEnv()` — **только из окружения процесса**,
  никакого файла конфигурации не подгружается. `viper` используется ровно в одном файле
  (`cmd/bot/main.go`) и больше нигде в репозитории.
- `cmd/db-create/main.go:16` жёстко зашит `sql.Open("sqlite3", "./db.sqlite3")` — путь к БД
  **не конфигурируется вовсе**, определяется текущим рабочим каталогом.
- `cmd/db-create/main.go` читает sidecar-файлы из `os.Args[1]`, то есть **ему нужен доступ к
  каталогу библиотеки**, а не только к каталогу с БД.
- Все три бинаря при ошибке завершаются `return` из `main` → **код возврата 0**.
  `cmd/scan/main.go:27` вдобавок делает `_ = s.Scan()`, глуша ошибку всего прохода.
- `cmd/bot/main.go:59` уже ждёт `<-quit` после `signal.Notify(…, SIGTERM)`, то есть
  graceful shutdown в зародыше есть, но оформлен неявно и без `stop_grace_period`.
- `cmd/bot/main.go:34` — `slog.Error("failed to init storage", err)`: `err` передаётся
  голым аргументом. Проверено: `go vet ./...` и `go test ./...` падают с
  `slog.Error arg "err" should be a string or a slog.Attr`, `go build ./...` проходит.
- `github.com/mattn/go-sqlite3` требует cgo → `CGO_ENABLED=1` и C-тулчейн в образе.

### 1.2. Гигиена репозитория: факты, расходящиеся с задачей

Задача (FR8) утверждает, что `db.sqlite3` (~324 МБ) и бинарь `db-create` «закоммичены в
git». Проверено — **это не так**:

- `git ls-files` не содержит ни `db.sqlite3`, ни `db-create`, ни `*.log`.
- `git log --all --diff-filter=A --name-only` показывает, что эти файлы **никогда не
  добавлялись** в индекс.
- Размер `.git` — **1.9 МБ**.
- `git check-ignore -v`: `db.sqlite3` игнорируется правилом `*.sqlite3`, `log.log` и
  `scan-books.log` — правилом `*.log`, `.env` — правилом `.env`.
- **Не игнорируются:** `db-create` (корень), `cmd/db-create/db-create`, а также
  `*.zip.json` и каталог `data/`, которые появятся после переезда на compose.
- Локально в рабочем дереве лежат: `db.sqlite3` (324 МБ), `db-create` (7.5 МБ),
  `cmd/db-create/db-create` (7.5 МБ), `log.log`, `scan-books.log`.

Вывод: FR8 переформулируется с «убрать из git» на «убрать с диска + закрыть
`.gitignore» + верифицировать, что в индексе чисто». Данные при этом **не теряются**:
`db.sqlite3` воспроизводится из sidecar-файлов, которые, в свою очередь, восстанавливаются
из ZIP-архивов сканированием.

---

## 2. Проблема

Невозможно развернуть проект в проде по письменной инструкции: нет `Dockerfile`, нет
compose-файла, нет шаблона окружения, нет скриптов. Пайплайн запускается вручную,
конфигурация неявная, ошибки конфигурации приводят к тихому успеху (код 0), часть ошибок
логируется в файлы, а `go vet ./...`/`go test ./...` не проходят из-за бага в `cmd/bot`.

---

## 3. Драйверы решения

| # | Драйвер | Источник |
|---|---|---|
| D1 | Один compose-файл на три бинаря; `up -d` поднимает только бота | FR2 |
| D2 | `scan` и `db-create` — сервисы compose, запускаемые разово | FR2, «Решения заказчика» |
| D3 | cgo-совместимая многоступенчатая сборка, минимальный разумный образ | FR1, NFR |
| D4 | Вся конфигурация — в `.env`, подстановка через `${ПЕРЕМЕННАЯ}` | FR3 |
| D5 | Понятные имена переменных; предсказуемое поведение при старых именах | FR4 |
| D6 | Отсутствие обязательной переменной → понятная ошибка + ненулевой код возврата | FR4, UC5 |
| D7 | Скрипты без сборочных зависимостей на хосте | FR5 |
| D8 | Только stdout/stderr, никаких файлов логов | FR6 |
| D9 | RW/RO-монтирования каталогов, пути из `.env` | FR7 |
| D10 | Никаких файлов приложения в рабочем каталоге контейнера | FR6, NFR |
| D11 | Корректная остановка по SIGTERM, без restart-loop | NFR |
| D12 | Секреты не в образах, не в git | NFR |
| D13 | Никаких изменений в бизнес-логике поиска, скачивания, сканирования | NFR |

---

## 4. Результаты исследования среды (эмпирическая проверка)

Все утверждения ниже проверены на целевой версии Docker Engine 29.2.1 / Compose v5.1.0
в отдельном scratch-каталоге; репозиторий не затрагивался.

| # | Проверка | Результат | Влияние на решение |
|---|---|---|---|
| E1 | Монтирование **несуществующего каталога**-источника | Docker **молча создаёт** пустой каталог на хосте, владелец `root` | Опечатка в пути не даёт ошибки, а даёт «библиотеку без книг». Нужна явная валидация в приложении |
| E2 | Монтирование **несуществующего файла**-источника | Docker создаёт на его месте **каталог** | Прямой bind-mount файла БД непригоден: SQLite получит «is a directory». Монтировать нужно каталог |
| E3 | `docker compose build` при сервисе с `profiles` | Профильный сервис **не собирается**; собирается только `bot` | Критерий приёмки «`docker compose build` собирает все три» неверен. `build.sh` обязан использовать `--profile tools build` |
| E4 | `docker compose up -d` при сервисах с `profiles` | Профильные сервисы **не создаются и не стартуют** | `profiles` — единственный механизм исключения сервиса из `up` |
| E5 | `docker compose run --rm <профильный-сервис>` | Работает **без** указания `--profile` | Скрипты `scan.sh` / `db-create.sh` могут быть однострочными |
| E6 | Код возврата `docker compose run` | Пробрасывается точно (проверены 0 и 3) | Критерий «возвращает код возврата контейнера» выполняется автоматически |
| E7 | `restart: unless-stopped` на одноразовом сервисе | Бесконечный restart-loop (`Restarting (1) 2 seconds ago`) | Одноразовым сервисам обязателен `restart: "no"` |
| E8 | `restart: unless-stopped` + бота, падающего из-за конфигурации | Тот же restart-loop, противоречит NFR «не должен падать в restart-loop» | Нужен ограниченный `restart: on-failure:5` |
| E9 | Синтаксис `${VAR:?msg}` | Поддерживается, ошибка на этапе интерполяции | **Не применяется** (см. §6, R10): ошибка возникает вне логов контейнера, ломает сборку без секрета |
| E10 | Вложенная интерполяция `${DB_DSN:-${DATA_CONTAINER_DIR:-/data}/${DB_FILE:-db.sqlite3}}` | Поддерживается, дефолты и переопределение работают | Позволяет вывести DSN без дублирования в `.env`, оставив возможность переопределения |
| E11 | Многоступенчатая сборка `golang:1.24-bookworm` → `debian:bookworm-slim`, `CGO_ENABLED=1`, cgo-бинарь | Собирается, работает; финальный образ **138 МБ**; `ldd` показывает только `libc.so.6` | Дизайн Dockerfile подтверждён; `libsqlite3` в образе не нужен (amalgamation линкуется в бинарь) |
| E12 | `debian:bookworm-slim` и CA-сертификаты | `ca-certificates.crt` **отсутствует** | Ставить CA-сертификаты нужно только в стадии `bot` — это единственный сетевой компонент |
| E13 | `mattn/go-sqlite3 v1.14.24`: параметры DSN | `_journal_mode=WAL` применяется и в форме пути, и в форме `file:` URI; `busy_timeout` по умолчанию **уже 5000 мс** | `?_journal_mode=WAL` можно задать прямо в `.env`; `busy_timeout` задавать не нужно |
| E14 | `go build ./...` / `go vet ./...` / `go test ./...` в текущем HEAD | `build` — ок; `vet` — падает на `cmd/bot/main.go:34`; `test` — падает на `cmd/bot` (build failed) **и** на `format/fb2.TestReadFb2` | Второе падение — предсуществующее и не связано с задачей: тест читает `/home/drypa/Downloads/fb2-113437-119690/114594.fb2` (абсолютный путь вне репозитория), файл в git модифицирован не нами |
| E15 | Полный скелет compose со всеми предлагаемыми конструкциями (`x-` якорь, `profiles`, `on-failure:5`, `stop_grace_period`, `read_only`, `tmpfs`, `cap_drop`, `security_opt`, `init`, длинный синтаксис `volumes`) | `docker compose config -q` — валидно; `config --services` без профиля показывает только `bot` | Все конструкции приняты; перечисление сервисов в диагностике требует `--profile tools` |

---

## 5. Решение

### 5.1. Структура файлов

```
book-shelf/
├── Dockerfile                     # NEW  единый рецепт, 4 стадии (build + 3 рантайм-цели)
├── .dockerignore                  # NEW  исключает .git, db.sqlite3, data/, бинари
├── docker-compose.yml             # NEW  три сервиса; scan/db-create под profiles: ["tools"]
├── .env.example                   # NEW  шаблон с комментариями; .env — в .gitignore
├── scripts/
│   └── common.sh                  # NEW  общие хелперы для 4 скриптов (DRY)
├── build.sh                       # NEW  docker compose --profile tools build
├── up.sh                          # NEW  docker compose up -d
├── scan.sh                        # NEW  docker compose --profile tools run --rm scan
├── db-create.sh                   # NEW  docker compose --profile tools run --rm db-create
├── README.md                      # NEW  инструкция по развёртыванию
├── internal/
│   ├── config/config.go           # NEW  чтение/валидация переменных + фолбэк на старые имена
│   └── logging/logging.go         # NEW  настройка slog + мост stdlib log → slog
├── cmd/bot/main.go                # MOD  переименование переменных, коды возврата, SIGTERM
├── cmd/bot/bot.go                 # MOD  log.Printf → slog (FR6)
├── cmd/scan/main.go               # MOD  коды возврата, проброс ошибки Scan()
├── cmd/db-create/main.go          # MOD  DSN из окружения вместо "./db.sqlite3", коды возврата
├── scanner/scan.go                # MOD  fmt/log → slog (опционально, см. план работ, задача T-13)
├── format/fb2/fb2.go              # MOD  fmt.Println → slog.Error (опционально, T-13)
├── .gitignore                     # MOD  бинари, data/, sidecar-файлы, db.sqlite3-семья
├── AGENTS.md                      # MOD  новые имена переменных, скрипты, ссылка на README
├── go.mod / go.sum                # MOD  удаляется github.com/spf13/viper
└── docs/adr/0001-… , docs/plans/  # NEW
```

`internal/config` и `internal/logging` — не форма ради формы: семантика фолбэка на старые
имена, формат сообщения об ошибке и настройка логирования должны быть **идентичными** во
всех трёх бинарях, иначе они разъедутся при первом же расхождении (DRY, а не YAGNI —
дублируется в трёх местах).

### 5.2. Dockerfile

Один файл, одна build-стадия на все три бинаря, три лёгкие рантайм-стадии. Так рецепт
сборки живёт в одном месте (нет трёх копий для синхронизации), и при этом каждый сервис
получает **собственный минимальный образ только со своим бинарём**.

```dockerfile
# syntax=docker/dockerfile:1
# Требуется BuildKit (Docker 23+). Кэш-монты не влияют на корректность, только на скорость.

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
```

Намеренные решения:

- **glibc (bookworm), не musl (alpine).** `go-sqlite3` через cgo на musl собирается, но
  требует `gcc musl-dev` и известен проблемами с линковкой; цена экономии ~30 МБ не
  оправдывает риск для прода. Проверено фактически (E11).
- **В образе нет `libsqlite3`**: драйвер включает amalgamation SQLite в бинарь, из
  динамических библиотек остаётся только `libc.so.6` (E11).
- **`USER app:app`** по умолчанию везде. Специальный пользователь для `scan` задаётся в
  compose (см. §5.4) — так uid не «зашит» в образ, и оператор подгоняет его под
  владельца каталога на хосте без пересборки.
- **`ENTRYPOINT` на бинарь, аргументы — в `command` сервиса.** Это даёт одновременно
  корректную сигнатуру (`PID 1` получает SIGTERM — важно для D11) и возможность
  переопределить аргумент без правки Dockerfile.
- `ENTRYPOINT` в exec-форме — процесс идёт **без шелла-обёртки**, поэтому SIGTERM
  доходит до Go-программы, а не к `/bin/sh`.

### 5.3. docker-compose.yml

```yaml
name: ${COMPOSE_PROJECT_NAME:-book-shelf}

# Общий блок логирования: DRY, три бинаря одинаково.
x-logging: &logging
  LOG_LEVEL: "${LOG_LEVEL:-info}"
  LOG_FORMAT: "${LOG_FORMAT:-json}"
  TZ: "${TZ:-UTC}"

services:
  # ──────────────────────────────── бот: единственный долгоживущий сервис
  bot:
    image: book-shelf-bot:latest
    build:
      context: .
      dockerfile: Dockerfile
      target: bot
    restart: on-failure:5          # не бесконечный restart-loop (E8), но переживает ребут хоста
    init: true                     # reaper зомби; ловит SIGTERM и передаёт PID 1
    stop_grace_period: 20s
    stop_signal: SIGTERM
    read_only: true                # бот не пишет файлы (FR6); tgbotapi отдаёт FileBytes из памяти
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop: [ ALL ]
    security_opt: [ "no-new-privileges:true" ]
    environment:
      <<: *logging
      BOT_TOKEN: "${BOT_TOKEN:-}"                  # валидируется приложением, см. §5.6
      LIBRARY_DIR: "${LIBRARY_CONTAINER_DIR:-/library}"
      DB_DSN: "${DB_DSN:-${DATA_CONTAINER_DIR:-/data}/${DB_FILE:-db.sqlite3}}"
      HTTP_PROXY: "${HTTP_PROXY:-}"
    volumes:
      - type: bind
        source: "${LIBRARY_HOST_DIR:-./data/library}"
        target: "${LIBRARY_CONTAINER_DIR:-/library}"
        read_only: true                            # боту достаточно чтения архивов
      - type: bind
        source: "${DATA_HOST_DIR:-./data/db}"
        target: "${DATA_CONTAINER_DIR:-/data}"

  # ──────────────────────────────── scan: одноразовый, RW в каталог библиотеки
  scan:
    image: book-shelf-scan:latest
    build:
      context: .
      dockerfile: Dockerfile
      target: scan
    profiles: [ "tools" ]         # ← единственный способ исключить сервис из `up` (E4)
    restart: "no"                 # ← иначе restart-loop (E7)
    init: true
    user: "${SCAN_USER_ID:-10001}:${SCAN_GID:-10001}"
    read_only: true
    tmpfs:
      - /tmp:size=256m,mode=1777   # сюда scanner делает os.MkdirTemp под распаковку
    cap_drop: [ ALL ]
    security_opt: [ "no-new-privileges:true" ]
    environment:
      <<: *logging
      SCAN_PARALLELISM: "${SCAN_PARALLELISM:-5}"
    volumes:
      - type: bind
        source: "${LIBRARY_HOST_DIR:-./data/library}"
        target: "${LIBRARY_CONTAINER_DIR:-/library}"   # RW: пишет .zip.json (FR7)
    command: [ "${LIBRARY_CONTAINER_DIR:-/library}" ]   # аргумент в compose, не в скрипте (см. §6, R6)

  # ──────────────────────────────── db-create: одноразовый, читает sidecar'ы, пишет БД
  db-create:
    image: book-shelf-db-create:latest
    build:
      context: .
      dockerfile: Dockerfile
      target: db-create
    profiles: [ "tools" ]
    restart: "no"
    init: true
    user: "${APP_USER_ID:-10001}:${APP_GID:-10001}"
    read_only: true
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop: [ ALL ]
    security_opt: [ "no-new-privileges:true" ]
    environment:
      <<: *logging
      DB_DSN: "${DB_DSN:-${DATA_CONTAINER_DIR:-/data}/${DB_FILE:-db.sqlite3}}"
    volumes:
      - type: bind
        source: "${LIBRARY_HOST_DIR:-./data/library}"   # ← нужен: читает *.zip.json (FR7, уточнение)
        target: "${LIBRARY_CONTAINER_DIR:-/library}"
        read_only: true
      - type: bind
        source: "${DATA_HOST_DIR:-./data/db}"
        target: "${DATA_CONTAINER_DIR:-/data}"
    command: [ "${LIBRARY_CONTAINER_DIR:-/library}" ]
```

Ключевые решения и их обоснование:

1. **`profiles: ["tools"]` + `restart: "no"`** — пара, без которой требования FR2 и NFR
   невыполнимы. `profiles` исключает сервис из `up` (E4), `restart: "no"` не даёт
   одноразовым задачам уйти в restart-loop (E7). `docker compose run` при этом работает
   без указания профиля (E5), а код возврата пробрасывается точно (E6).
2. **`db-create` монтирует каталог библиотеки read-only.** Это **уточнение к FR7**: задача
   перечисляет для `db-create` только БД, но `cmd/db-create/main.go:34` читает
   `*.zip.json` именно из каталога библиотеки. Без этого монтирования сервис
   неработоспособен. Решение — добавить RO-монтирование, а не копировать sidecar'ы в
   каталог с БД.
3. **Каталог с БД монтируется, а не файл.** Проверено: bind-mount несуществующего файла
   приводит к созданию **каталога** на его месте (E2), что ломает SQLite; вдобавок для
   WAL нужны соседние `-wal`/`-shm`, которые не переживут file-монтирование.
4. **Никаких `${VAR:?}`** (см. §6, R10) — вместо этого валидация в приложении.
5. **`BOT_TOKEN` не попадает в образ и в git**: он передаётся только через
   `environment` из `.env`. Диагностика выполняется командой `docker compose config -q`
   (quiet), а не `docker compose config`, чтобы токен не печатался в терминал.
6. **`read_only: true` + `tmpfs`** — прямое исполнение FR6/NFR: в рабочем каталоге
   контейнера не может появиться ни одного файла. `scan` получает увеличенный `/tmp`
   (256 МБ), потому что `scanner.processArchive` распаковывает архивы в `os.MkdirTemp("")`.
7. **`depends_on` не используется намеренно.** Цепочка
   `bot ← db-create ← scan` с `service_completed_successfully` заставила бы `up -d`
   выполнять весь пайплайн, что прямо противоречит FR2.
8. **Никаких healthcheck-ов.** YAGNI: единственный способ проверки живости бота —
   `/start` в Telegram (UC6) и `docker compose ps`; healthcheck потребовал бы второго
   кодового пути («режим проверки» в бинаре) или сетевого вызова из контейнера.

### 5.4. Переменные окружения

#### 5.4.1. Итоговый перечень

| Переменная | Читает | Обяз. | Дефолт | Назначение |
|---|---|:--:|---|---|
| `BOT_TOKEN` | `bot` | **да** | — | Токен от @BotFather |
| `HTTP_PROXY` | `bot` | нет | *(пусто)* | Прокси к `api.telegram.org`; пусто → прямое соединение |
| `DB_DSN` | `bot`, `db-create` | **да** | `/data/db.sqlite3?_journal_mode=WAL` | DSN для `sql.Open("sqlite3", …)`; переопределяет вывод ниже |
| `DATA_CONTAINER_DIR` | compose | нет | `/data` | Точка монтирования каталога БД в контейнере |
| `DB_FILE` | compose | нет | `db.sqlite3` | Имя файла БД внутри `DATA_CONTAINER_DIR` |
| `LIBRARY_CONTAINER_DIR` | compose | нет | `/library` | Точка монтирования каталога архивов в контейнере |
| `LIBRARY_HOST_DIR` | compose | нет | `./data/library` | Каталог с `.zip` на хосте (bind-источник) |
| `DATA_HOST_DIR` | compose | нет | `./data/db` | Каталог с БД на хосте (bind-источник) |
| `SCAN_PARALLELISM` | `scan` | нет | `5` | Одновременная обработка архивов |
| `SCAN_USER_ID` / `SCAN_GID` | compose | нет | `10001` / `10001` | uid:gid для контейнера `scan` |
| `APP_USER_ID` / `APP_GID` | compose | нет | `10001` / `10001` | uid:gid для `db-create` |
| `LOG_LEVEL` | все три | нет | `info` | `debug`\|`info`\|`warn`\|`error` |
| `LOG_FORMAT` | все три | нет | `json` | `json`\|`text` |
| `TZ` | все три | нет | `UTC` | Часовой пояс (нужен пакет `tzdata` в образе) |
| `COMPOSE_PROJECT_NAME` | compose | нет | `book-shelf` | Префикс имён контейнеров, сети и томов |

Инвариант: `DB_DSN` по умолчанию вычисляется compose как
`${DATA_CONTAINER_DIR}/${DB_FILE}` (E10). Нет дублирования в `.env`; при необходимости
задать DSN-параметры оператор просто раскомментирует в `.env.example` строку `DB_DSN=` и
задаёт её явно — она имеет приоритет.

#### 5.4.2. Переименование и обратная совместимость

| Старое имя | Новое имя в коде | Решение |
|---|---|---|
| `DB_PATH` | **`LIBRARY_DIR`** | фолбэк: `LIBRARY_DIR` → `DB_PATH` |
| `DB_CONNECTION_STRING` | **`DB_DSN`** | фолбэк: `DB_DSN` → `DB_CONNECTION_STRING` |
| `BOT_TOKEN` | `BOT_TOKEN` | без изменений |
| `HTTP_PROXY` | `HTTP_PROXY` | без изменений |
| `SCAN_PARALLELISM` | `SCAN_PARALLELISM` | без изменений |

**Решение: совместимость сохраняется как явный фолбэк с предупреждением, а не как
жёсткий переход.**

Правило разрешения (одно на все переменные, реализуется в `internal/config`):

1. Если задано **новое** имя — используется оно. Старое имя, даже если задано,
   игнорируется **молча** (но это единственный «тихий» случай; он безопасен: новое
   имя всегда явно введено оператором и имеет приоритет).
2. Если новое имя **не задано**, но задано старое — используется старое, в лог
   пишется `WARN` вида `DEPRECATED: DB_PATH is deprecated, use LIBRARY_DIR`.
3. Если не задано ни то, ни другое — фатальная ошибка с ненулевым кодом возврата и
   перечислением **всех** недостающих переменных сразу, а не по одной.

Почему фолбэк, а не жёсткий переход:

- Старое имя используется в **любых** существующих ручных запусках (shell, systemd-юнит,
  локальные скрипты оператора) — их нельзя сломать без согласованного окна работ.
- Стоимость — ~15 строк в одном пакете плюс предупреждение в логах; выгода — отсутствие
  «тихого использования неверного пути», которого требует edge case задачи.
- Старые имена **не попадают** в compose и `.env.example`: compose требует только новые
  имена, поэтому оператор с забытым старым `.env` получает громкую ошибку
  «`LIBRARY_CONTAINER_DIR` … default»/валидацию приложения, а не молчаливое поведение.

Дедлайн удаления фолбэка фиксируется отдельной задачей (см. план работ, задача T-16), чтобы
совместимость не стала вечной.

### 5.5. Скрипты

Все скрипты: `#!/usr/bin/env bash`, `set -euo pipefail`, `cd` в корень репозитория,
работа **только** через `docker compose` (никаких `source .env`, никакого Go на хосте).

**`scripts/common.sh`** — общий преамбл (иначе 4× дублирование проверок):

```bash
#!/usr/bin/env bash
# Общие хелперы для build.sh / up.sh / scan.sh / db-create.sh.
# Источник: build.sh up.sh scan.sh db-create.sh

COMPOSE_PROFILES="tools"

compose() { docker compose "$@"; }

require_env_file() {
  if [ ! -f .env ]; then
    echo "ОШИБКА: файл .env не найден." >&2
    echo "Создайте его из шаблона:  cp .env.example .env" >&2
    echo "и заполните как минимум BOT_TOKEN." >&2
    exit 1
  fi
}

# -q (quiet) печатает ТОЛЬКО ошибки интерполяции/синтаксиса compose и ничего успешного.
# Это важно: `docker compose config` без -q напечатал бы BOT_TOKEN в терминал.
preflight() {
  require_env_file
  compose config -q
}
```

**`build.sh`**

```bash
#!/usr/bin/env bash
# Сборка образов всех трёх сервисов: bot, scan, db-create.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
. ./scripts/common.sh

preflight
# --profile tools ОБЯЗАТЕЛЕН: docker compose build без профиля НЕ собирает
# сервисы scan и db-create (проверено на Compose v5.1.0).
compose --profile "$COMPOSE_PROFILES" build
compose --profile "$COMPOSE_PROFILES" config -q

echo "Готово: образы book-shelf-bot, book-shelf-scan, book-shelf-db-create собраны."
echo "Запуск бота:            ./up.sh"
echo "Пополнение библиотеки:  ./scan.sh && ./db-create.sh"
```

**`up.sh`**

```bash
#!/usr/bin/env bash
# Поднимает ТОЛЬКО бота (сервисы scan и db-create скрыты профилем "tools").
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
. ./scripts/common.sh

preflight
compose up -d

echo "Статус:  docker compose ps"
echo "Логи:    docker compose logs -f bot"
```

**`scan.sh`**

```bash
#!/usr/bin/env bash
# Разовый запуск контейнера сканирования. Аргумент (каталог библиотеки) задан
# в compose как command сервиса scan — здесь он не дублируется.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
. ./scripts/common.sh

preflight
# --rm        : удалить одноразовый контейнер после завершения
# --no-deps   : не поднимать другие сервисы, даже если появится depends_on
# -T          : без псевдо-TTY, чтобы логи оставались машинно-читаемыми
# Код возврата контейнера пробрасывается автоматически (set -e).
compose --profile "$COMPOSE_PROFILES" run --rm --no-deps -T scan

echo "Сканирование завершено. Следующий шаг: ./db-create.sh"
```

**`db-create.sh`**

```bash
#!/usr/bin/env bash
# Разовый запуск контейнера наполнения базы из *.zip.json.
# ВНИМАНИЕ: повторный запуск ДУБЛИРУЕТ строки (см. README, раздел «Переиндексация»).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
. ./scripts/common.sh

preflight
compose --profile "$COMPOSE_PROFILES" run --rm --no-deps -T db-create

echo "База наполнена. Если бот запущен — перезапустите его: ./up.sh"
```

Почему аргументы лежат в compose, а не в скриптах: скрипту потребовалось бы разобрать
`.env`, чтобы узнать путь; единственный способ сделать это в bash — `source .env`, что
выполняет произвольный shell-код из пользовательского файла и ломается на значениях с
пробелами и спецсимволами (`DB_DSN` с `?` и `&`). Размещение аргумента в compose даёт
единственный источник истины без парсинга (см. §6, R6).

### 5.6. Изменения в Go-коде

Объём минимальный, бизнес-логика не затрагивается (D13).

**`internal/config/config.go`** (новый)

```go
// Package config читает переменные окружения приложения.
//
// Правила:
//   - обязательная переменная отсутствует → ошибка и ненулевой код возврата;
//   - все отсутствующие переменные сообщаются ОДНОЙ ошибкой, а не по одной;
//   - для переименованных переменных поддерживается фолбэк на старое имя
//     с предупреждением об устаревании.
package config

// Get возвращает primary, если она задана; иначе legacy с предупреждением;
// иначе "".
func Get(primary, legacy string) string

// Require проверяет, что все имена заданы (с учётом legacy-фолбэка).
// Возвращает ошибку вида: `missing required environment variables: BOT_TOKEN, DB_DSN`.
func Require(names ...string) error
```

**`internal/logging/logging.go`** (новый)

```go
// Package logging настраивает структурированное логирование в stdout.
//
// slog пишет JSON (или text) в os.Stdout. Стандартный лог, которым пользуется
// scanner/scan.go, перенаправляется в slog через slog.NewLogLogger — поэтому
// существующие вызовы log.Printf становятся структурированными без правок
// бизнес-кода.
func Setup()
```

**`cmd/bot/main.go`**

- Исправить `slog.Error("failed to init storage", err)` → `slog.Error("failed to init storage", "err", err)` (D: снимает падение `vet`/`test`).
- Структура `main`: `func main() { if err := run(); err != nil { slog.Error("fatal", "err", err); os.Exit(1) } }` — **единая точка ненулевого выхода** вместо пяти разных `return`.
- `logging.Setup()` первым вызовом.
- Чтение конфигурации — **до** любых побочных эффектов (сейчас `DB_PATH`/`BOT_TOKEN`
  проверяются раньше `DB_CONNECTION_STRING`, из-за чего ошибка в DSN обнаруживается
  после `os.Stat` каталога).
- `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` вместо ручного канала;
  после отмены контекста — `db.Close()` и `slog.Info("shutdown complete")`.
- Убрать `github.com/spf13/viper` (используется только здесь; `os.LookupEnv` даёт точные
  семантики обязательности, которых у viper нет).

**`cmd/scan/main.go`**

- Отсутствие аргумента → usage в stderr и `os.Exit(2)`.
- `_ = s.Scan()` → `if err := s.Scan(); err != nil { slog.Error(...); os.Exit(1) }`.
- Ненулевой код на общем сбое прохода; покадровые ошибки внутри `Scan()` остаются
  **нефатальными** и логируются (это зафиксированное поведение пайплайна).

**`cmd/db-create/main.go`**

- `sql.Open("sqlite3", "./db.sqlite3")` → `sql.Open("sqlite3", config.Require("DB_DSN") …)`.
- Все `fmt.Println(err); return` → `return err` с кодом 1.
- `fmt.Printf("processing %s\n", …)` → `slog.Info("processing sidecar", "path", …)` (FR6).

**`internal/logging` + точечная замена `fmt.Print*` → `slog`** в `cmd/db-create/main.go`,
`cmd/bot/bot.go`, `format/fb2/fb2.go`, `scanner/scan.go` — это наблюдаемость, а не бизнес-логика;
вынесено в отдельную опциональную задачу T-13, чтобы не смешивать с обязательным минимумом.

### 5.7. Логирование и коды возврата

- Единый формат: `slog` → `os.Stdout`, JSON по умолчанию, уровень из `LOG_LEVEL`.
- Стандартный `log` (им пользуется `scanner/scan.go`) перенаправляется в `slog` через
  `slog.NewLogLogger`, поэтому `log.Printf("processing archive %s", …)` уже является
  структурированной записью; `log.log` и `scan-books.log` больше не появляются нигде.
- Никаких `> file.log` в скриптах: единственный источник логов — драйвер Docker
  (`json-file` по умолчанию); доступ через `docker compose logs`.
- **Коды возврата:** `0` — успех (включая «частично обработанную библиотеку», где ошибки по
  отдельным архивам пропущены и залогированы); `1` — фатальная ошибка конфигурации или
  пайплайна; `2` — неверные аргументы командной строки. Ненулевой код возврата
  пробрасывается из контейнера наружу через `docker compose run` (E6), поэтому скрипты
  корректно падают при `set -e`.

### 5.8. Graceful shutdown и политика перезапуска

| Аспект | Решение |
|---|---|
| Сигнал | `stop_signal: SIGTERM` (по умолчанию) + `init: true` — `tini` пересылает сигнал PID 1 |
| Обработка | `signal.NotifyContext` → отмена контекста → горутина `processNotifications` выходит через `select <-ctx.Done()` (механизм в `bot.go:75` уже готов) → `db.Close()` → `slog.Info("shutdown complete")` |
| Таймаут | `stop_grace_period: 20s` с запасом: `GetUpdatesChan` находится в long-poll, отмена контекста его не прерывает, но процесс завершается сразу, не дожидаясь ответа Telegram |
| Политика бота | `restart: on-failure:5` — переживает ребут хоста и транзиентные сбои (сеть, недоступность Telegram), но **не** уходит в бесконечный restart-loop при ошибке конфигурации (E8) |
| Диагностика | `docker compose ps` покажет `Exited (1)` после 5 неудачных попыток; состояние и причина — в `docker compose logs bot` |
| Одноразовые | `restart: "no"` (E7) |

Компромисс, осознанно принятый: различить «транзиентный сбой» и «ошибка конфигурации»
политикой рестарта нельзя. `on-failure:5` — компромисс; оператор после `Exited (1)`
запускает `./up.sh`. Альтернатива «бот живёт вечно и ждёт конфигурацию» отвергнута
(§6, R17) — она маскирует ошибку, которую задача (UC5) требует показывать.

### 5.9. Гигиена репозитория

С учётом §1.2 (артефакты **не** в индексе git) реальная работа — это `.gitignore` +
удаление с диска + верификация:

Дополнить `.gitignore`:

```gitignore
# ── сборка Go ──────────────────────────────────────────────
/bot
/scan
/db-create
/cmd/bot/bot
/cmd/scan/scan
/cmd/db-create/db-create
/bin/
/dist/

# ── рабочие данные (создаются на хосте, монтируются в контейнеры) ──
/data/
*.zip.json
db.sqlite3*          # -wal, -shm, -journal; правило *.sqlite3 их НЕ покрывает

# ── окружение ──────────────────────────────────────────────
.env
.env.*
!.env.example
```

Уже покрыто существующими правилами и дублировать не нужно: `*.log`, `*.sqlite3`, `.env`.

Действия с данными: `db.sqlite3` (324 МБ) **не удаляется** до последнего шага миграции и
до бэкапа вне репозитория. Он воспроизводим (`rm` + `./db-create.sh` по имеющимся
sidecar'ам, либо полный `./scan.sh` + `./db-create.sh`), но 324 МБ ради этого держать в
рабочем дереве незачем.

### 5.10. README.md

Обязательные разделы (сверх перечисленных в FR9):

- Требования: Docker Engine 23+ (нужен BuildKit для кэш-монтиров), Compose v2.
- Порядок первичного развёртывания: каталоги на хосте → `cp .env.example .env` → заполнить
  → `./build.sh` → `./up.sh`.
- **Таблица всех переменных** с назначением и дефолтами.
- Описание четырёх скриптов и того, что они делают и чего не делают.
- ⚠️ **Предупреждение о записи в каталог библиотеки**: `scan` монтирует его RW и создаёт
  рядом с архивами `.zip.json`; меняются права/владелец на хосте, поэтому нужен
  `sudo chown -R 10001:10001 ./data/library` (или задать `SCAN_USER_ID`/`SCAN_GID`).
- ⚠️ **Предупреждение о дублировании строк** при повторном `db-create` и рекомендуемый
  порядок переиндексации: `./up.sh` → … → остановить бота (`docker compose stop bot`) →
  `rm -f ./data/db/db.sqlite3*` → `./db-create.sh` (sidecar'ы уже есть) → `./up.sh`.
- Диагностика: `docker compose ps`, `docker compose logs -f bot`, `docker compose logs`
  после `scan.sh`, ручная проверка `/start` в Telegram.
- Бэкап: `sqlite3 data/db/db.sqlite3 ".backup 'backup.db'"` (безопасно на горячей базе)
  либо копирование при остановленном боте.
- Примечание: состояние поиска каждого пользователя живёт только в памяти и **теряется при
  перезапуске** бота (существующее поведение).
- Изменение поведения `HTTP_PROXY`: в контейнер попадает **только** явно заданное в `.env`
  значение; `HTTP_PROXY`, экспортированный в shell хоста, больше не подхватывается
  автоматически (это исправление, а не регрессия — см. §7, риск K7).

---

## 6. Рассмотренные и отвергнутые варианты

| # | Вариант | Вердикт | Обоснование |
|---|---|---|---|
| R1 | Три отдельных `Dockerfile` (`Dockerfile.bot`, …) | ❌ | Три копии одного рецепта, неизбежно расходятся (тэги, флаги сборки, пользователь). Один файл с тремя `target`-стадиями даёт тот же результат без дублирования. |
| R2 | Один образ со всеми тремя бинарями, переключение через `command:` | ❌ | Слабее изоляция: любой контейнер может запустить любой бинарь; образ содержит ненужные бинари; не выполняет критерий «`build` собирает образы всех трёх сервисов». |
| R3 | Без `profiles`, полагаясь только на `restart: "no"` | ❌ | `docker compose up -d` поднимет все сервисы без исключения. `profiles` — единственный механизм исключения (проверено E4). |
| R4 | Цепочка `depends_on: {db-create: {condition: service_completed_successfully}}` | ❌ | Превратила бы `up -d` в полный пайплайн, прямо нарушая FR2 и решение заказчика. |
| R5 | Bind-mount **файла** БД (`${DB_HOST_FILE}:/data/db.sqlite3`) | ❌ | Docker создаст на месте отсутствующего файла **каталог** (E2) → SQLite падает с невнятной ошибкой; `-wal`/`-shm` не переживут file-монтирование; оператор обязан заранее создавать пустой файл. Каталог монтировать правильнее. |
| R6 | Скрипты делают `set -a; . ./.env; set +a` | ❌ | Выполняет shell-код из пользовательского файла; ломается на значениях с пробелами, кавычками и `?`/`&` в DSN. Аргументы вынесены в `command` compose — источник истины один. |
| R7 | Сборка бинарей на хосте + копирование в `scratch`-образ | ❌ | Нарушает «скрипты не должны собирать Go на хосте» (FR5) и требует тулчейна у оператора. |
| R8 | Базовый образ на Alpine/musl | ❌ | cgo+musl требует `gcc musl-dev` и известен флейками; экономия ~30 МБ не окупает риск для прода. Проверено, что glibc-вариант работает (E11). |
| R9 | `gcr.io/distroless/base-debian12:nonroot` | ❌ (рассмотрен) | Образ ~35 МБ вместо 138 МБ, но: внешний реестр `gcr.io` в рантайме, хуже диагностика (`docker run ... sh` невозможен), неочевидное владение uid. Возвращаться как отдельная оптимизация после замеров. |
| R10 | `${BOT_TOKEN:?BOT_TOKEN обязателен}` в compose | ❌ | Ошибка возникает на этапе интерполяции — **вне логов контейнера**, что прямо противоречит UC5 («ошибка видна в логах контейнера»); кроме того, делает `docker compose build` зависимым от секрета, а сборка должна быть воспроизводимой без секретов (NFR). Валидация переносится в приложение. |
| R11 | Сделать `db-create` идемпотентным (`UNIQUE`-ограничение / upsert / флаг `--reset`) | ❌ (в этой итерации) | Меняет бизнес-логику и схему данных, запрещено D13, требует миграции существующей БД на 324 МБ. Решение документируется в README как известное ограничение; вынесено в «будущая работа». |
| R12 | `restart: unless-stopped` для бота | ❌ | При ошибке конфигурации — бесконечный restart-loop (E8), что прямо нарушает NFR. |
| R13 | Сохранить `viper` | ❌ | Единственное место использования — `cmd/bot/main.go`; файл конфигурации всё равно не подгружается, а `os.LookupEnv` даёт точные семантики обязательности, фолбэка и сообщений об ошибках. Убрать зависимость. |
| R14 | Docker named volumes вместо bind mounts | ❌ | Оператор не видит ZIP-архивы и БД на хосте, бэкап и перенос усложняются; FR7 подразумевает пути на хосте из `.env`. |
| R15 | Писать логи в файлы внутри volume | ❌ | Нарушает FR6/NFR; логи должны идти в stdout и читаться через `docker compose logs`. |
| R16 | Жёсткий переход на новые имена без фолбэка | ❌ | Ломает существующие ручные запуски без возможности узнать об этом заранее. Фолбэк + `WARN` + дедлайн удаления (§5.4.2). |
| R17 | Бот не завершается при ошибке конфигурации, а ждёт | ❌ | Маскирует ошибку и противоречит UC5/критерию «ненулевой код возврата». |
| R18 | Healthcheck-сервиса бота | ❌ | Требует либо сетевого вызова к Telegram из контейнера, либо второго кодового пути в бинаре. YAGNI для сервиса с одним процессом. |
| R19 | `scan` и `db-create` как `docker run` без compose-сервисов | ❌ | Нарушает FR2 («присутствуют в compose как сервисы») и теряет единый источник volumes/окружения. |

---

## 7. Риски и митигации

| # | Риск | Вероятность | Влияние | Митигация |
|---|---|---|---|---|
| K1 | Опечатка в `LIBRARY_HOST_DIR`/`DATA_HOST_DIR` → Docker молча создаст пустой каталог от `root` (E1), оператор не заметит | высокая | среднее | Приложение валидирует: `scan` — «каталог не найден» **или** «найдено 0 архивов `*.zip`» → код 1; `db-create` — «найдено 0 sidecar'ов `*.zip.json`, сначала запустите ./scan.sh» → код 1; `bot` — ошибка на несуществующем/не-каталоге (уже есть), предупреждение на пустом. README: раздел «Диагностика» с `ls -la` и `docker compose run --rm scan` |
| K2 | `docker compose build` без `--profile` молча соберёт только `bot` (E3); критерий приёмки в задаче сформулирован неверно | высокая | среднее | `build.sh` всегда использует `--profile tools`; критерий приёмки исправлен (§11, критерий 2) |
| K3 | `db-create` дублирует строки при повторном запуске | высокая | высокое | Явное предупреждение в README и в тексте `db-create.sh`; рекомендованный порядок переиндексации с остановленным ботом; рекомендация не запускать `db-create.sh` без изменения библиотеки |
| K4 | Конкурентный доступ к SQLite (бот читает, `db-create` пишет) → `database is locked` | средняя | среднее | `?_journal_mode=WAL` в `DB_DSN` (E13) + `busy_timeout` 5000 мс по умолчанию у драйвера; в README — «останавливайте бота перед переиндексацией» |
| K5 | WAL требует mmap/shared-memory; на NFS/SMB без `lockd` не заработает | низкая | высокое | Каталог БД монтируется, а не файл; в README — «размещайте `DATA_HOST_DIR` на локальной ФС (ext4/xfs), не на сетевой» |
| K6 | Root-овнерство sidecar-файлов и каталога на хосте после `scan` | высокая | среднее | Непривилегированный пользователь по умолчанию (uid 10001) + `SCAN_USER_ID`/`SCAN_GID` в `.env`; в README — `chown` и объяснение последствий |
| K7 | Поведение `HTTP_PROXY` меняется: прокси из shell хоста больше не наследуется | средняя | среднее | Явно задокументировано в README как исправление; `HTTP_PROXY` остаётся в `.env.example` (UC4) |
| K8 | Фолбэк на старые имена env закрепится навсегда | средняя | низкое | Отдельная задача T-16 с удалением фолбэка; `WARN` в логах делает устаревшие запуски заметными |
| K9 | Предсуществующий падающий тест `format/fb2.TestReadFb2` (абсолютный путь вне репозитория) мешает «зелёному» `go test ./...` (E14) | высокая | низкое | Вне объёма ADR; вынесено в T-14 (пометить `t.Skip` или положить тестфикстуру в репозиторий). Явно зафиксировано, чтобы не сочли регрессией |
| K10 | Рост размера образов (138 МБ × 3 общих слоя) | средняя | низкое | Стадия `runtime` переиспользуется между тремя финальными образами — на диске это практически один базовый слой; при необходимости — возврат к distroless (R9) |
| K11 | Непривилегированный uid 10001 не совпадает с владельцем каталога на хосте → падение `scan` по правам | средняя | среднее | Это **ожидаемое** поведение (edge case задачи); в README — точная команда `chown` и объяснение; настройка через `.env` без пересборки |
| K12 | Потеря состояния поиска при перезапуске бота | высокая | низкое | Существующее поведение (состояние в памяти); отражено в README |
| K13 | Удаление `db.sqlite3` (324 МБ) до бэкапа | низкая | высокое | Порядок фаз §8: удаление — **последним** шагом, после бэкапа вне репозитория и после успешной проверки `./scan.sh && ./db-create.sh` |

---

## 8. План миграции

Каждая фаза — отдельный коммит, независимо откатывается. Фазы 1–2 обязательны до
первого запуска в проде; фазы 5–6 выполняются после успешного `./up.sh`.

| Фаза | Содержимое | Откат |
|---|---|---|
| **0. Подготовка** | Бэкап текущего `db.sqlite3` **вне репозитория**; фиксация `git rev-parse HEAD`; копия текущих `.env`/окружения хоста, если есть | — |
| **1. Гигиена репозитория** | Расширить `.gitignore`; удалить `db-create`, `cmd/db-create/db-create`, `log.log`, `scan-books.log` с диска; **проверить** `git ls-files` на отсутствие артефактов | `git revert`, файлы восстановить из бэкапа |
| **2. Исправления кода** | `internal/config`, `internal/logging`; фикс `slog.Error` в `cmd/bot/main.go:34`; коды возврата во всех трёх бинарях; переименование переменных с фолбэком; `DB_DSN` вместо `./db.sqlite3` в `db-create`; `signal.NotifyContext`; удаление `viper` | `git revert` — **старые имена продолжат работать** благодаря фолбэку |
| **3. Инфраструктура** | `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example`, `scripts/common.sh`, `build.sh`, `up.sh`, `scan.sh`, `db-create.sh` | удаление файлов; compose-деплой ещё не использовался |
| **4. Документация** | `README.md`; обновление `AGENTS.md` (новые имена переменных, скрипты, ссылка на README); ADR в `docs/adr` | `git revert` |
| **5. Верификация** | `go build ./...`, `go vet ./...`, `go test ./archive/... ./finder/... ./storage/...`; `./build.sh`; `./up.sh`; `docker compose ps` (только бот `Up`); `./scan.sh`; `./db-create.sh`; ручная проверка `/start`, поиск, `/get` в Telegram | — |
| **6. Удаление артефакта** | После успешной фазы 5 удалить локальный `db.sqlite3` из рабочего дерева (данные воспроизводимы, бэкап есть) | вернуть из бэкапа |

Критический путь: 0 → 2 → 3 → 5. Фазы 1 и 4 независимы от 2–3 и могут выполняться
параллельно.

---

## 9. План отката

**Уровень 1 — откат кода (без потери данных).** Фаза 2 откатывается одним коммитом;
старые имена переменных (`DB_PATH`, `DB_CONNECTION_STRING`) продолжают работать, потому
что фолбэк оставлен именно для этого. Конфигурация хоста не требует изменений.

**Уровень 2 — откат инфраструктуры.** `docker compose down` (Compose v5: без флага `-q`),
затем `git revert` фазы 3. Возврат к ручному запуску бинарей: `go build ./cmd/...` и
запуск с **старыми** именами переменных. Данные на хосте (`data/library`, `data/db`)
не трогаются — compose их только монтирует.

**Уровень 3 — откат данных.** Единственная необратимая операция во всём плане —
удаление `db.sqlite3` из рабочего дерева (фаза 6). Защита: бэкап вне репозитория (фаза 0)
+ воспроизводимость (`./db-create.sh` по имеющимся sidecar'ам). Для полного
воспроизведения из исходников: `./scan.sh && ./db-create.sh` (при остановленном боте).

**Чего откат не делает:** не откатывает уже выполненную переиндексацию — если строки
продублировались, единственный путь — удалить БД и пересоздать ( задокументировано
в README).

---

## 10. Последствия

**Положительные**

- Развёртывание воспроизводится по README без ручной работы на хосте.
- Конфигурация проверяется явно: отсутствие обязательной переменной даёт код 1 и
  понятное сообщение вместо тихого успеха.
- `go vet ./...` и `go test ./...` проходят по `cmd/bot`.
- Логи единообразны и доступны через `docker compose logs`; файлов приложения на диске
  контейнера нет.
- Контейнеры непривилегированные, `cap_drop: ALL`, `read_only: true`.
- Сборка воспроизводима: `-trimpath`, зафиксированные теги базовых образов, зависимости
  в отдельном слое.

**Отрицательные / издержки**

- Три сервиса вместо трёх бинарей — появляется понятие «собери образы» (компенсируется
  `./build.sh`).
- `build.sh` обязан использовать `--profile tools` — неочевидно, задокументировано в
  самом скрипте и в README.
- Требование Docker Engine 23+ (BuildKit) из-за кэш-монтиров.
- Бот ограничен `read_only: true` — любая будущая запись файлов потребует явного
  пересмотра.
- Каталог библиотеки на хосте получает root- или 10001-овнерные sidecar'ы — это цена
  требования «scan работает с произвольным каталогом оператора».
- Появляется новый слой конфигурации (`.env`), который нужно синхронизировать с compose;
  ошибки возможны, но диагностируются (`docker compose config -q`).

---

## 11. Критерии приёмки

Критерии заданы в разделе «Критерии приёмки» задачи. Два из них требуют **уточнения** по
результатам исследования, остальные переносятся без изменений.

| № | Критерий | Статус |
|---|---|---|
| 1 | Есть Dockerfile с cgo, собирающий `bot`, `scan`, `db-create` | без изменений |
| 2 | ~~`docker compose build` собирает образы всех трёх сервисов~~ → **`docker compose --profile tools build`** | **исправлен:** без `--profile` профильные сервисы не собираются (E3). Именно это делает `build.sh` |
| 3 | `./build.sh` собирает все три сервиса, код 0 | без изменений |
| 4 | `./up.sh` = только `docker compose up -d`, поднимает только бота | без изменений |
| 5 | `docker compose ps` показывает только запущенный бот | без изменений |
| 6 | `./scan.sh` разово запускает `scan`, возвращает код контейнера | без изменений (E6) |
| 7 | `./db-create.sh` разово запускает `db-create`, возвращает код контейнера | без изменений (E6) |
| 8 | Есть `.env.example` со всеми переменными и комментариями | без изменений |
| 9 | Все значения через `${ПЕРЕМЕННАЯ}`, включая пути к каталогам | без изменений (E10) |
| 10 | `.env` в `.gitignore` | без изменений (уже выполнено) |
| 11 | `DB_PATH` → понятное имя, `DB_CONNECTION_STRING` → понятное; все упоминания согласованы | без изменений; решение — `LIBRARY_DIR` и `DB_DSN` (§5.4.2) |
| 12 | Каталог архивов: RW в `scan`, RO в `bot` | без изменений; **дополнительно**: RO в `db-create` (уточнение FR7) |
| 13 | Путь к БД монтируется в `bot` и `db-create` | без изменений; монтируется **каталог**, а не файл (§5.3, п.3) |
| 14 | В рабочем каталоге контейнеров нет файлов логов | без изменений; усилено `read_only: true` |
| 15 | Отсутствие обязательной переменной → понятное сообщение + ненулевой код | без изменений |
| 16 | `go build ./...` проходит | без изменений |
| 17 | `go vet ./...` и `go test ./...` не падают на `cmd/bot` | без изменений; **отдельно** зафиксирован предсуществующий сбой `format/fb2.TestReadFb2` (E14) |
| 18 | `db.sqlite3`, бинарь `db-create`, `*.log`, `.env` не отслеживаются; `.gitignore` дополнен | **переформулирован:** они не отслеживались и раньше (§1.2); критерий — «артефакты удалены с диска, `.gitignore` закрывает `data/`, `*.zip.json`, бинари и `db.sqlite3*`, `git ls-files` чист» |
| 19 | README со всеми перечисленными разделами | без изменений |

---

## 12. Связанные документы

- Задача: [`docs/tasks/docker-compose-deploy.md`](../tasks/docker-compose-deploy.md)
- План работ: [`docs/plans/README.md`](../plans/README.md) и файлы `docs/plans/*.md`
- Руководство оператора: `README.md` (создаётся в фазе 4)
- Агентские заметки: `AGENTS.md` (обновляются в фазе 4)
