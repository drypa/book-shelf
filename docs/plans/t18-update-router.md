# T-18. Роутер обновлений и устойчивость к паникам (фикс H1)

- **Приоритет:** P0 (предпосылка всего контроля доступа)
- **Оценка:** 1 день
- **Зависит от:** —
- **Блокирует:** T-21, T-22
- **Основание:** ADR-0003 § 1.3, § 4.3 (R0–R3, M1–M2)

---

## Цель

Сделать обработку обновлений Telegram не падающей на не-`message` обновлениях
и заложить точку ветвления (message / callback / ignore), в которую T-21/T-22
встраивают контроль доступа. В текущем коде `cmd/bot/bot.go:60` безусловно
разыменовывает `update.Message`; `telegram-bot-api v4.6.4` не отправляет
`allowed_updates`, поэтому `edited_message`/`callback_query` приходят уже
сейчас и валят бот panic'ом (H1).

## Что делать

1. **Переписать `processNotifications`:**
   - каждый апдейт обрабатывается функцией с `defer recover()`; panic →
     `slog.Error("panic while handling update", "update_id", …, "err", …)`,
     цикл продолжается;
   - порядок маршрутизации (ADR § 4.3): `CallbackQuery != nil` → callback-ветка;
     `Message != nil` → message-ветка; иначе `slog.Debug` + ignore
     (`edited_message`, `channel_post`, пустой update);
   - message-ветка до диспетчера: `From == nil` → ignore + `slog.Warn`;
     `!msg.Chat.IsPrivate()` → ignore + `slog.Debug`.
2. **В callback-ветке (заглушка до T-22):** `slog.Debug` +
   `AnswerCallbackQuery` (пустой ack), чтобы у клиента не крутился «часы».
   Никаких действий, кроме ack.
3. **Логика `actions`-мапы и обработчики команд не меняются.** Разбор команды
   и вызов `a(msg.Text, b, update)` остаются ровно как были; удаляется только
   конструация «эхо»-`NewMessage` из update-полей (используется лишь `getCommand(update.Message.Text)`).
4. **Тестируемость:** вынести классификацию в чистую функцию вида
   `classifyUpdate(u tgbotapi.Update) updateKind` и решение message-ветки
   (`from == nil`, не-private) в отдельную функцию — табличные тесты без сети.

## Тесты (`cmd/bot/bot_test.go`)

- `classifyUpdate`: message-only, callback-only, edited_message, пустой update →
  ожидаемая ветка.
- P1–M2-решение: `From == nil`, group chat → ignore.
- Panic в обработчике не прерывает цикл (обработка «ядовитого» update, затем
  нормального).

## Критерии приёмки

- [ ] `edited_message`/`callback_query` не роняют бота (ручная проверка: в
      личных сообщениях отредактировать своё сообщение — бот жив, в логах только debug).
- [ ] Сообщения в группу игнорируются.
- [ ] Поведение одобренного потока (все пять команд) не изменилось.
- [ ] `go build ./... && go vet ./... && go test ./...` зелёные.

## Риски

| Риск | Митигация |
|---|---|
| Неосознанно «разрешить» то, что раньше падало (например, эхо-ответ на не-сообщение) | Р3 — явный ignore c debug-логом, никаких новых side effects |

## Связанные документы

- ADR: [`docs/adr/0003-access-control.md`](../adr/0003-access-control.md) § 1.3, § 4.3
- План: [`docs/plans/access-control.md`](access-control.md)
