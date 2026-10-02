# AGENTS.md

Personal ebook library: Telegram bot that searches FB2 books stored in ZIP archives,
plus a two-step offline indexing pipeline. No README — this file is the only guide.

## Pipeline (order matters, each step is a separate binary)

1. `cmd/scan` — `go run ./cmd/scan <lib-dir>` walks `<lib-dir>` for `*.zip`, unzips each
   to a temp dir, parses every `*.fb2`, and writes a metadata sidecar `<archive>.zip.json`
   **next to the original zip** (it mutates the library directory). `SCAN_PARALLELISM`
   env var, default 5. Errors are logged per-archive and skipped, not fatal.
2. `cmd/db-create` — `go run ./cmd/db-create <lib-dir>` reads `*.zip.json` sidecars and
   INSERTs rows into `books` in `./db.sqlite3`, **relative to the current working
   directory**, not the module root. Run it from wherever you want the DB to land.
3. `cmd/bot` — the long-running Telegram bot serving searches/downloads.

Re-running `db-create` **duplicates rows**: the insert has no unique constraint or upsert.
Wipe the DB and rescan when metadata changes; do not "re-run to refresh".

## Bot environment variables (cmd/bot/main.go)

- `BOT_TOKEN` — required.
- `DB_PATH` — **directory containing the `.zip` archives**, despite the name. It is
  passed to `newStorage()`; the bot resolves a book as `<DB_PATH>/<archive column>`.
- `DB_CONNECTION_STRING` — required — the actual SQLite DSN for `sql.Open("sqlite3", ...)`.
- `HTTP_PROXY` — optional, e.g. `http://host:port`.

All read via `viper.AutomaticEnv()` from the **process environment only** — there is no
`.env` file loading and no config file, despite viper being used. Because the proxy is
read as a bare `HTTP_PROXY`, any proxy exported in the shell silently routes Telegram
traffic through it.

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

- CI (`.github/workflows/go.yml`) only runs `go build -v ./cmd/bot/...`. Use
  `go build ./...` locally to cover all three binaries.
- Targeted tests: `go test ./archive/... ./finder/... ./storage/...`.
- `github.com/mattn/go-sqlite3` requires cgo — `CGO_ENABLED=1` and a C toolchain.
- Known pre-existing vet failure, unrelated to your change:
  `cmd/bot/main.go:34` passes `err` as a bare arg to `slog.Error`. `go vet ./...` and
  `go test ./...` report `cmd/bot [build failed]` because of it; `go build` does not.