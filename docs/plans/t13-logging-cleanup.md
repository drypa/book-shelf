# T-13. Дочистка логирования: `fmt.Print*`/`log.Printf` → `slog`

- **Приоритет:** P2
- **Оценка:** 0.5 дня
- **Зависит от:** T-01
- **Блокирует:** —
- **Основание:** ADR-0001 §5.6, §5.7

---

## Цель

Убрать оставшиеся неструктурированные выводы, чтобы **все** сообщения трёх бинарей
попадали в stdout в едином формате.

## Контекст

T-01 перенаправляет стандартный `log` в `slog`, поэтому `log.Printf` уже дают
структурированные записи. Но `fmt.Print*` идут мимо `slog` и остаются
неструктурированными (и, как правило, в stderr). FR6 требует «все сообщения … в
структурированном виде».

Осознанно вынесено из обязательного минимума, чтобы не смешивать инфраструктурные
задачи с косметикой: FR6 закрывается и без этого шага.

## Что делать

Затронутые места (проверить `grep -rn "fmt\.Print" --include=*.go .`):

| Файл | Что | Заменить на |
|---|---|---|
| `scanner/scan.go` | `log.Printf("failed to process archive %s: %s", …)` | `slog.Error("failed to process archive", "archive", name, "err", err)` |
| `scanner/scan.go` | `log.Printf("processing archive %s", name)` | `slog.Info("processing archive", "archive", name)` |
| `scanner/scan.go` | `log.Printf("Scanning %s\n", path)` | `slog.Debug("scanning archive", "path", path)` (либо `Info`) |
| `scanner/scan.go` | `log.Printf("failed to read metadata from %s: %s", …)` | `slog.Warn(...)` |
| `scanner/scan.go` | `log.Printf("failed to read fb2 file %s: %v", …)` | `slog.Warn(...)` |
| `cmd/bot/bot.go:222` | `log.Println(err)` в `sendResponse` | `slog.Error("send response failed", "err", err)` |
| `format/fb2/fb2.go:85` | `fmt.Println("Error decoding XML:", err)` | `slog.Error("error decoding XML", "err", err)` |
| `cmd/db-create/main.go` | `fmt.Printf("file %s has no title\n", …)` | `slog.Warn("book has no title info", "file", …)` |
| `cmd/scan/main.go`, `cmd/bot/main.go` | уже переведены в T-03…T-05 | — |

Правила:

- не менять **условия** и **порядок** логирования — только форму вывода;
- не менять уровень по умолчанию для сообщений об ошибках обработки отдельных книг:
  они должны оставаться видимыми на уровне `info`/`warn`;
- после правок из `scanner/scan.go` не должно остаться импортов `log` (проверить
  `go vet`).

## Критерии приёмки

- [ ] `grep -rn "fmt\.Print" --include=*.go .` — пусто (кроме осознанно оставленного в
      тестах, если есть).
- [ ] `grep -rn "log\.Print" --include=*.go .` — пусто.
- [ ] `go build ./...`, `go vet ./...` проходят.
- [ ] Все сообщения в логах контейнера — валидный JSON (при `LOG_FORMAT=json`).
- [ ] Ошибки по отдельным архивам по-прежнему видны на уровне `info`/`warn`.

## Риски и примечания

- `scanner/scan.go` — общая библиотека пайплайна; изменение уровней логирования может
  повлиять на «шум» в логах `scan`. Не понижать уровень сообщений об ошибках архивов.
- Не выполнять эту задачу в том же коммите, что T-01: сначала убедиться, что
  перенаправление `log` → `slog` работает (T-01), затем переводить остальное.
