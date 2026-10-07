# AGENTS.md

Personal ebook library: Telegram bot that searches FB2 books stored in ZIP archives,
plus a two-step offline indexing pipeline.

- Operator guide (deployment, env vars, diagnostics): `README.md`.
- Technical decision behind the container layout: `docs/adr/0001-docker-compose-deployment.md`.
- Task breakdown: `docs/plans/`. This file is the map for agents working in the code.

## Pipeline (order matters, each step is a separate binary)

1. `cmd/scan` — walks a library dir for `*.zip`, unzips each to a temp dir, parses every
   `*.fb2`, and writes a metadata sidecar `<archive>.zip.json` **next to the original zip**
   (it mutates the library directory). `SCAN_PARALLELISM` env var, default 5. Errors are
   logged per-archive and skipped, not fatal. Exit code: `2` usage, `1` failure or an
   empty library, `0` success.
2. `cmd/db-create` — reads `*.zip.json` sidecars and INSERTs rows into `books` at `DB_DSN`.
   Exit code: `2` usage, `1` failure, `0` success.
3. `cmd/bot` — the long-running Telegram bot serving searches/downloads.

Each step is a separate binary and also a separate container:

| Step | Native (dev loop) | Container (prod-like) |
|---|---|---|
| scan | `go run ./cmd/scan ./data/library` | `./scan.sh` |
| db-create | `go run ./cmd/db-create ./data/library` with `DB_DSN=./data/db/db.sqlite3` | `./db-create.sh` |
| bot | `go run ./cmd/bot` with `LIBRARY_DIR`/`DB_DSN`/`BOT_TOKEN` set | `./up.sh` |

Native runs use the **host** paths; containers always use the container paths
(`/library`, `/data`) because that is what compose mounts. Those container paths are
hard-coded in `docker-compose.yml` — there is no env var for them.

Re-running `db-create` **duplicates rows**: the insert has no unique constraint or upsert.
Wipe the DB and rescan when metadata changes; do not "re-run to refresh". Exact commands
are in `README.md` §"Повторный `db-create` дублирует строки".

## Container deploy (scripts)

```bash
./build.sh     # docker compose --profile tools build  (--profile is mandatory: without it
               # scan and db-create are not built, see ADR §4, E3)
./up.sh        # docker compose up -d — bot only; scan/db-create hide behind the tools profile
./down.sh      # docker compose down — stop bot, remove containers + project network
./scan.sh      # one-shot, RW bind into the library dir, creates *.zip.json
./db-create.sh # one-shot, duplicates rows on re-run
```

Every script requires `.env` next to `docker-compose.yml` and exits 1 with a message if it
is missing. Warnings about `scan` mutating the library and about duplicated rows live in
`README.md` §6–§7.

Gotchas worth knowing before touching compose:

- Only `bot` is long-running. `scan` and `db-create` carry `profiles: ["tools"]` +
  `restart: "no"`; that is the only way to keep them out of `up` (E4) and out of a
  restart loop (E7). `bot` uses `restart: on-failure:5`, so it stops as `Exited (1)`
  after 5 failed starts instead of looping forever (E8).
- `bot` mounts the library **read-only** and the DB directory read-write; `scan` mounts
  the library read-write; `db-create` reads sidecars from the library (RO) and writes
  the DB dir (RW). All three bind into the fixed container paths `/library` and `/data`.
  The DB is bind-mounted as a **directory**, not a file: SQLite needs
  `-wal`/`-shm` next to it, and bind-mounting a missing file creates a directory.
- All services run unprivileged as uid/gid `10001` with `read_only` rootfs, a tmpfs
  `/tmp` (`scan` needs 256m for `os.MkdirTemp`), `cap_drop: ALL` and
  `no-new-privileges`. Host bind sources must be writable by that uid — see
  `README.md` step 4.
- `TZ`, `LOG_LEVEL` and `LOG_FORMAT` are shared through the `x-logging` anchor; only
  `bot` additionally gets the CA bundle (`/etc/ssl/certs` is bot-only).
- `.env.example` is the single source of truth for variables and defaults.

## Bot environment variables (cmd/bot/main.go, internal/config)

- `BOT_TOKEN` — required.
- `LIBRARY_DIR` — **directory containing the `.zip` archives** (container path, `/library`).
  It is passed to `newStorage()`; the bot resolves a book as `<LIBRARY_DIR>/<archive column>`.
- `DB_DSN` — required — the SQLite DSN for `sql.Open("sqlite3", ...)`; in compose it is
  computed as `/data/${DB_FILE}` (container path `/data` is fixed).
- `HTTP_PROXY` — optional, e.g. `http://host:port`. Only the value passed into the
  container counts; a proxy exported in the host shell is no longer picked up (K7).
- `LOG_LEVEL`, `LOG_FORMAT`, `TZ` — optional, consumed by `internal/logging`.

Everything is read from the **process environment** by `internal/config` (no `.env`
loading in the app, no config file, viper removed). Compose reads `.env` for the host
side; the app only ever sees what compose injected. Missing required variables are
reported together in one `missing required environment variables: …` message and exit 1.

Legacy names `DB_PATH` and `DB_CONNECTION_STRING` are still accepted as **deprecated
fallbacks** (`internal/config.LegacyEnv`); they warn once per name. `.env.example` uses
the current names only — do not add new code against the legacy ones.

Book lookup depends on the DB rows matching the library exactly: the `archive` column is
the zip's base name (no `.zip`) and `file_name` must equal the zip entry name verbatim —
`archive.UnzipFile` compares `f.Name` for equality, not suffix.

## Bot command flow

State is per-user and lives only in memory (`Searcher` in cmd/bot/searcher.go), so it is
lost on restart. Dispatch table is `actions` in cmd/bot/bot.go:

- `/start` begins a search (required before any other command).
- `/author <text>` / `/title <text>` update the field and immediately search, page size 10.
- `/result` re-runs the search with current fields.
- `/get [N]` sends book N from the last result page; N is 1-based and optional only when
  the page holds exactly one result. Upload name comes from `Book.GetDownloadFileName()`.

## Layout notes

- `storage/book.go` — `storage.Book` is the shared row model used by both the pipeline and
  the bot. Change columns here plus the SELECT lists in `cmd/bot/repository.go` **and**
  `finder/db/db.go`, which must stay in sync with the `books` table.
- `finder/` is **legacy/unused** — nothing imports it; its test is its only consumer. The
  bot searches through `cmd/bot/repository.go` instead. Do not wire new code to `finder/db`.
- `format/fb2` — FB2 parsing with a `CharsetReader` whitelist (`charMap`, cp1251/1252/1255,
  ISO-8859-1/5, KOI8-R). Adding an encoding means adding to that map; the comparison is
  lowercased on both sides.

## Build / verify

```bash
CGO_ENABLED=1 go build ./...            # all three binaries
CGO_ENABLED=1 go vet ./...              # clean, incl. the former cmd/bot slog.Error failure
CGO_ENABLED=1 go test ./...             # all packages green, incl. format/fb2 fixtures
gofmt -l .                              # must print nothing
docker compose --profile tools build    # all three images
```

- CI (`.github/workflows/go.yml`) mirrors the Go steps above (gofmt, build, vet, test) in
  the `build` job, and builds the three images in a separate `images` job. No secrets are
  referenced in CI.
- Targeted tests: `go test ./archive/... ./finder/... ./storage/... ./internal/...`.
- `github.com/mattn/go-sqlite3` requires cgo — `CGO_ENABLED=1` and a C toolchain.
- `format/fb2` tests are hermetic: fixtures live in `format/fb2/testdata` (utf-8, cp1251,
  koi8-r). Machine-local scenarios are kept but disabled by the `_` prefix
  (`_TestReadAllFb2`, `_TestReadFb2Local`) — run those by hand only.