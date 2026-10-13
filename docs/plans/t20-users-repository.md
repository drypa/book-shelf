# T-20. Таблица `users` + `AccessRepository` + тесты

- **Приоритет:** P0 (FR2, FR3, FR5)
- **Оценка:** 1 день
- **Зависит от:** —
- **Блокирует:** T-21, T-22
- **Основание:** ADR-0003 § 4.1, § 4.5

---

## Цель

Персистентное состояние пользователей (заявка/одобрен/отклонён) в существующей
SQLite-базе: схема, репозиторий и атомарные переходы. Таблица принадлежит боту
и создаётся им самим при старте; `books` и пакет `storage` не меняются.

## Что делать

1. **Новый файл `cmd/bot/access.go`:**

   ```go
   type AccessStatus string
   const (
       StatusNone      AccessStatus = ""         // строки нет
       StatusPending   AccessStatus = "pending"
       StatusApproved  AccessStatus = "approved"
       StatusDeclined  AccessStatus = "declined"
   )

   type AccessRepository struct{ db *sql.DB }
   func NewAccessRepository(db *sql.DB) *AccessRepository

   // EnsureSchema — CREATE TABLE IF NOT EXISTS users (...); вызывается из run().
   // Status         — SELECT status; sql.ErrNoRows -> StatusNone.
   // CreatePending  — INSERT ... ON CONFLICT(telegram_id) DO NOTHING;
   //                  возвращает created=true, если строка появилась.
   // Decide         — UPDATE users SET status=?, decided_at=?
   //                  WHERE telegram_id=? AND status='pending';
   //                  возвращает (applied bool, current AccessStatus).
   ```

   Схема — дословно из ADR § 4.1 (`telegram_id INTEGER PRIMARY KEY`,
   `status TEXT NOT NULL CHECK (...)`, снимок профиля, `created_at`,
   `decided_at`). timestamps — `time.Now().UTC().Format(time.RFC3339)`.

2. **`cmd/bot/main.go`:** после успешного `db.Ping()` вызвать
   `EnsureSchema()`; ошибка → `fmt.Errorf("init users schema: %w", err)`
   (бот не стартует). Созданный `AccessRepository` хранить в переменной и
   передать в `newBot` (используется в T-21/T-22).

Таблица **не** создаётся в `cmd/db-create`, не попадает в `storage`.

## Тесты (`cmd/bot/access_test.go`, SQLite `:memory:`)

- `EnsureSchema` идемпотентен (два вызова без ошибки); состав статусов — CHECK
  отклоняет произвольное значение.
- `Status` несуществующего → `StatusNone`.
- `CreatePending`: первый вызов `created=true`; повтор → `created=false`,
  строка не изменилась (данных заявки — нет дублей, FR2).
- `Decide`: из `pending` → `applied=true`, статус сменился, `decided_at`
  заполнен; повторный `Decide` → `applied=false, current=approved`;
  `declined` → approve → `applied=false, current=declined` (terminal-статусы
  необратимы).

## Критерии приёмки

- [ ] Схема соответствует ADR § 4.1 (CHECK, PK без AUTOINCREMENT).
- [ ] Все переходы только из `pending`, атомарно (`RowsAffected`).
- [ ] Бот стартует на старой базе без `users` (таблица появляется), и повторный
      старт тоже ok.
- [ ] `go vet ./... && go test ./...` зелёные.

## Связанные документы

- ADR: [`docs/adr/0003-access-control.md`](../adr/0003-access-control.md) § 4.1, § 4.5
- План: [`docs/plans/access-control.md`](access-control.md)
