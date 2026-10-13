# T-19. `ADMIN_TELEGRAM_ID`: обязательная переменная, валидация, deploy

- **Приоритет:** P0 (FR7)
- **Оценка:** 0.5 дня
- **Зависит от:** —
- **Блокирует:** T-21
- **Основание:** ADR-0003 § 4.2

---

## Цель

Telegram ID единственного администратора становится обязательной конфигурацией:
без неё бот не стартует (fail-fast единым сообщением, как остальные
обязательные переменные), нечисловое значение — отдельная внятная ошибка.

## Что делать

1. **`cmd/bot/main.go` (`run`):**
   - `config.Required("BOT_TOKEN", "LIBRARY_DIR", "DB_DSN", "ADMIN_TELEGRAM_ID")`;
   - сразу после: `strconv.ParseInt(values["ADMIN_TELEGRAM_ID"], 10, 64)`;
     ошибка или значение `<= 0` →
     `fmt.Errorf("configuration: ADMIN_TELEGRAM_ID must be a positive integer, got %q", …)`;
   - `adminID int64` передать в `newBot` (в T-21 будет использоваться guard'ом).
   - API `internal/config` **не меняется** (`LegacyEnv` не пополняется).
2. **`.env.example`:** в секцию «обязательные» добавить:

   ```
   # Telegram ID единственного администратора (число). Узнать: написать,
   # например, @userinfobot. Без него бот не стартует:
   # «missing required environment variables: … ADMIN_TELEGRAM_ID».
   ADMIN_TELEGRAM_ID=
   ```

3. **`docker-compose.yml`:** в `bot.environment` добавить
   `ADMIN_TELEGRAM_ID: "${ADMIN_TELEGRAM_ID:-}"` (только `bot`; пустое
   значение проходит compose — валидирует приложение, принцип `BOT_TOKEN`).

## Тесты

- `cmd/bot/main_test.go`:
  - в `botEnv` добавить `ADMIN_TELEGRAM_ID` (cleanup);
  - обновить `TestRunReportsEveryMissingVariable`: ожидаемое сообщение теперь
    `missing required environment variables: BOT_TOKEN, LIBRARY_DIR, DB_DSN, ADMIN_TELEGRAM_ID`
    (порядок — как в вызове `Required`);
  - новый тест: `ADMIN_TELEGRAM_ID=abc` (остальное задано) → ошибка содержит
    `ADMIN_TELEGRAM_ID must be a positive integer`;
  - новый тест: `ADMIN_TELEGRAM_ID=0` → та же ошибка.
- `internal/config`: тесты не меняются (API тот же).

## Критерии приёмки

- [ ] Без `ADMIN_TELEGRAM_ID` бот завершается с exit 1 и сообщением,
      перечисляющим все отсутствующие переменные.
- [ ] Нечисловое/нулевое/отрицательное значение — отдельная внятная ошибка.
- [ ] compose передаёт переменную только сервису `bot`.
- [ ] `go vet ./... && go test ./...` зелёные.

## Связанные документы

- ADR: [`docs/adr/0003-access-control.md`](../adr/0003-access-control.md) § 4.2
- План: [`docs/plans/access-control.md`](access-control.md)
