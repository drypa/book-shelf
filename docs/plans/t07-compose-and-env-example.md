# T-07. `docker-compose.yml` и `.env.example`

- **Приоритет:** P0
- **Оценка:** 1 день
- **Зависит от:** T-06
- **Блокирует:** T-08, T-10, T-12
- **Основание:** ADR-0001 §5.3, §5.4.1

---

## Цель

Описать три бинаря как три сервиса в одном compose-файле так, чтобы `up -d` поднимал
только бота, а `scan`/`db-create` запускались разово — и чтобы ни один путь, токен или
режим не был зашит в файл.

## Контекст и обязательные находки

Три поведения Compose, установленные эмпирически (ADR §4) и определяющие конструкцию:

- **`profiles` — единственный способ исключить сервис из `up`** (E4). Сервисы
  `scan` и `db-create` обязаны быть под `profiles: ["tools"]`.
- **`docker compose build` без `--profile` НЕ собирает профильные сервисы** (E3) —
  собирается только `bot`. Отсюда `--profile tools` в `build.sh` (T-08) и исправленный
  критерий приёмки №2.
- **Bind-mount несуществующего источника молча создаёт каталог** на хосте (каталог — E1,
  а если «файл» — тоже каталог, E2). Поэтому каталог БД монтируется, а не файл, и
  валидация путей живёт в приложении (T-03…T-05).

## Что делать

1. Создать `docker-compose.yml` по спецификации ADR §5.3, без отклонений:

   - `name: ${COMPOSE_PROJECT_NAME:-book-shelf}`;
   - якорь `x-logging: &logging` с `LOG_LEVEL`, `LOG_FORMAT`, `TZ`, подключаемый
     через `<<: *logging` во все три сервиса (DRY);
   - **`bot`**: цель `bot`, `restart: on-failure:5`, `init: true`,
     `stop_grace_period: 20s`, `stop_signal: SIGTERM`, `read_only: true`,
     `tmpfs: /tmp:size=16m`, `cap_drop: [ALL]`, `security_opt: ["no-new-privileges:true"]`;
     `BOT_TOKEN: "${BOT_TOKEN:-}"` (без `:?` — см. ADR §6, R10), `LIBRARY_DIR:
     "${LIBRARY_CONTAINER_DIR:-/library}"`, `DB_DSN:
     "${DB_DSN:-${DATA_CONTAINER_DIR:-/data}/${DB_FILE:-db.sqlite3}}"`, `HTTP_PROXY: "${HTTP_PROXY:-}"`;
     монтирования: `LIBRARY_HOST_DIR` → `LIBRARY_CONTAINER_DIR` **RO**, `DATA_HOST_DIR` →
     `DATA_CONTAINER_DIR` RW; `depends_on` **не используется**;
   - **`scan`**: `profiles: ["tools"]`, `restart: "no"`, `init: true`,
     `user: "${SCAN_USER_ID:-10001}:${SCAN_GID:-10001}"`, `read_only: true`,
     `tmpfs: /tmp:size=256m` (под распаковку `os.MkdirTemp`), `SCAN_PARALLELISM`;
     монтирование `LIBRARY_HOST_DIR` → `LIBRARY_CONTAINER_DIR` **RW**;
     `command: ["${LIBRARY_CONTAINER_DIR:-/library}"]`;
   - **`db-create`**: `profiles: ["tools"]`, `restart: "no"`, `init: true`,
     `user: "${APP_USER_ID:-10001}:${APP_GID:-10001}"`, `read_only: true`, `DB_DSN`;
     монтирования: `LIBRARY_HOST_DIR` → `LIBRARY_CONTAINER_DIR` **RO**
     (**уточнение к FR7**: `cmd/db-create` читает `*.zip.json` именно оттуда) и
     `DATA_HOST_DIR` → `DATA_CONTAINER_DIR` RW; `command` с тем же каталогом.

   Длинный синтаксис `volumes:` (`type`/`source`/`target`/`read_only`) предпочтителен
   короткому `a:b:ro` — он читается однозначно и не путает `:ro` с частью пути.

2. Создать `.env.example` — шаблон с комментариями, **рабочими значениями по умолчанию**
   (FR3). Структура и точный набор переменных — таблица ADR §5.4.1. Обязательно:

   - сгруппировать с секционными комментариями (`# ── обязательные ──`,
     `# ── пути на хосте ──`, `# ── пути в контейнере ──`, `# ── приложение ──`,
     `# ── логирование ──`, `# ── compose ──`);
   - `BOT_TOKEN=` — пустое значение с комментарием «обязательно, получить у @BotFather»;
   - `LIBRARY_HOST_DIR=./data/library`, `DATA_HOST_DIR=./data/db`;
   - `LIBRARY_CONTAINER_DIR=/library`, `DATA_CONTAINER_DIR=/data`;
   - `DB_FILE=db.sqlite3`;
   - `DB_DSN=` — **закомментированная** строка с пояснением, что по умолчанию выводится
     как `${DATA_CONTAINER_DIR}/${DB_FILE}`, и раскомментировать её нужно только чтобы
     добавить DSN-параметры (например `?_journal_mode=WAL&_busy_timeout=5000`);
   - `SCAN_PARALLELISM=5`, `SCAN_USER_ID=10001`, `SCAN_GID=10001`,
     `APP_USER_ID=10001`, `APP_GID=10001`;
   - `LOG_LEVEL=info`, `LOG_FORMAT=json`, `TZ=UTC`, `COMPOSE_PROJECT_NAME=book-shelf`;
   - **не** упоминать старые имена `DB_PATH`/`DB_CONNECTION_STRING` — они живут только
     в коде как фолбэк (ADR §5.4.2).

3. Проверить синтаксис и поведение:

   ```bash
   docker compose config -q                     # -q: только ошибки, токен не печатается
   docker compose config --services             # ожидается ровно: bot
   docker compose --profile tools config --services   # ожидается: bot, scan, db-create
   ```

## Критерии приёмки

- [ ] `docker compose config -q` проходит без ошибок.
- [ ] Без `--profile` перечислен только `bot`; с `--profile tools` — все три.
- [ ] `docker compose up -d` создаёт **только** контейнер бота (`docker compose ps`).
- [ ] Ни одного пути, токена или DSN, зашитого в compose, — всё через `${…}` с дефолтами.
- [ ] `BOT_TOKEN` не появляется в выводе `docker compose config` без `-q` в документации
      как ожидаемое поведение (в скриптах используется только `-q`).
- [ ] В `.env.example` присутствуют все 15 переменных из таблицы ADR §5.4.1 с комментариями.
- [ ] Дефолты из `.env.example` рабочие: `cp .env.example .env` → `docker compose config -q`
      проходит без единой правки (кроме `BOT_TOKEN`, который проверяется приложением).

## Риски и примечания

- **`HTTP_PROXY` не наследуется из shell хоста** — в контейнер попадает только явно
  заданное значение. Это исправление (ADR §7, K7), но обязательно упоминается в README
  (T-10), иначе оператор решит, что прокси «сломался».
- `read_only: true` у всех трёх сервисов требует, чтобы ни один из них не писал файлы в
  рабочий каталог; это проверяется в T-12.
- Если в будущем появится `depends_on` — проверить, что он не протащит одноразовые
  сервисы в `up` (ADR §6, R4).
