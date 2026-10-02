# T-06. `Dockerfile` (многоступенчатый, cgo) и `.dockerignore`

- **Приоритет:** P0
- **Оценка:** 1 день
- **Зависит от:** —
- **Блокирует:** T-07, T-15
- **Основание:** ADR-0001 §5.2

---

## Цель

Собрать все три бинаря в Docker-образы с работающей поддержкой cgo, многоступенчатой
сборкой и минимально разумным финальным образом.

## Контекст

- `github.com/mattn/go-sqlite3` требует cgo: `CGO_ENABLED=1` и C-тулчейн в образе сборки.
- `go.mod` требует `go 1.24.0`.
- Проверено фактически на Docker Engine 29.2.1 / Compose v5.1.0: сборка
  `golang:1.24-bookworm` → `debian:bookworm-slim` даёт рабочий бинарь; финальный образ
  **138 МБ**, динамически слинкована только `libc.so.6` (SQLite amalgamation встроена в
  бинарь), `ca-certificates` в `debian:bookworm-slim` **отсутствует** (ADR §4, E11, E12).
- Требования к образам: секреты не запекаются, `ENTRYPOINT` в exec-форме (чтобы SIGTERM
  доходил до PID 1 — NFR graceful shutdown).

## Что делать

1. Создать `Dockerfile` **ровно по спецификации из ADR §5.2**:

   - `# syntax=docker/dockerfile:1` и `--mount=type=cache` — требуется BuildKit (Docker 23+);
   - `ARG GO_VERSION=1.24`, базовый образ `golang:${GO_VERSION}-bookworm`;
   - `ENV CGO_ENABLED=1 GOOS=linux`, `WORKDIR /src`;
   - `COPY go.mod go.sum ./` + `go mod download` отдельным слоем (правка `.go` не должна
     пересобирать модули);
   - `COPY . .` и `RUN` циклом по трём бинарям:
     `go build -trimpath -ldflags="-s -w" -o /out/<name> ./cmd/<name>`;
   - промежуточная стадия `runtime`: `tzdata`, непривилегированный
     `app:app` (uid/gid 10001), `ENV TZ=UTC`;
   - стадия `bot`: дополнительно `ca-certificates` (единственный сетевой компонент),
     затем `COPY --from=build /out/bot`, `USER app:app`,
     `ENTRYPOINT ["/usr/local/bin/bot"]`;
   - стадии `scan` и `db-create` — по одному бинарю каждая, тот же рантайм-слой.

2. Создать `.dockerignore` — **это не косметика**. Без него в контекст сборки попадают
   `.git`, `db.sqlite3` (324 МБ), `data/`, локальные бинари, `.env` с секретами:

   ```
   .git
   .github
   .idea
   .opencode
   docs
   data
   .env
   .env.*
   !.env.example
   *.sqlite3
   *.sqlite3-*
   *.zip.json
   *.log
   /bot
   /scan
   /db-create
   /cmd/*/bot
   /cmd/*/scan
   /cmd/*/db-create
   README.md
   ```

   Проверить фактический размер контекста: `docker build` не должен передавать сотни
   мегабайт.

3. Прогнать сборку всех трёх целей и убедиться, что бинарь запускается:

   ```bash
   docker build --target bot      -t book-shelf-bot:test .
   docker build --target scan     -t book-shelf-scan:test .
   docker build --target db-create -t book-shelf-db-create:test .
   docker run --rm book-shelf-db-create:test /каталог   # ждём usage с кодом 2
   ```

4. Проверить, что `db-create` в образе действительно работает с cgo: положить
   `*.zip.json` во временный каталог, задать `DB_DSN` и убедиться, что таблица создаётся
   (это проверка линковки SQLite, а не только факта запуска).

## Критерии приёмки

- [ ] `docker build --target <bot|scan|db-create>` собирает каждую цель отдельно.
- [ ] `scan` и `db-create` внутри образа реально работают с SQLite (cgo не сломан).
- [ ] `docker history` / `ldd` показывают отсутствие `libsqlite3` в образе.
- [ ] В финальных образах нет `.git`, `docs`, `.env`, `db.sqlite3`, `data/`.
- [ ] `id` внутри контейнера даёт uid 10001; `ENTRYPOINT` в exec-форме.
- [ ] `BOT_TOKEN` не встречается в слоях образа (в Dockerfile нет ни одного `ARG`/`ENV`
      с секретами и нет `COPY .env`).
- [ ] Контекст сборки — единицы мегабайт, а не сотни.

## Риски и примечания

- Кэш-монтиры требуют BuildKit; на очень старых хостах (Docker < 23) сборка упадёт —
  зафиксировать требование в README (T-10).
- Версию Go держать в одном месте (`ARG GO_VERSION`) и не повышать без проверки
  `go build ./...` локально: `go.mod` объявляет минимум 1.24.0.
- Alpine/musl сознательно **не** используется (ADR §6, R8): экономия ~30 МБ не окупает
  риски cgo на musl.
