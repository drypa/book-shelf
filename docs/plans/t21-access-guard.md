# T-21. AccessGuard в конвейере сообщений

- **Приоритет:** P0 (FR1, FR6; закрытие H2 для message-потока)
- **Оценка:** 1 день
- **Зависит от:** T-18, T-19, T-20
- **Блокирует:** T-22
- **Основание:** ADR-0003 § 4.3 (M3–M8)

---

## Цель

Ни одно сообщение неодобренного пользователя не достигает `actions`-мапы,
`repository.Search` и `storage.GetFile`. Одобрённый и администратор идут
существующим путём без изменений.

## Что делать

1. **В `Bot`** (`cmd/bot/bot.go`) поля `access *AccessRepository`,
   `adminID int64`; `newBot` принимает и сохраняет.
2. **Guard в message-ветке** (после M1/M2 из T-18, до `getCommand`/`actions`):

   ```go
   // псевдокод порядка (ADR § 4.3)
   if fromID == b.adminID            -> dispatch (как раньше)
   switch access.Status(fromID) {
   case approved:  -> dispatch (как раньше)
   case pending:   -> "Your registration request is pending approval."
   case declined:  -> "Access denied."
   case none:      -> "Access is closed. Press the button to request registration."
                      + inline-кнопка "Register" (callback data req:new, T-22)
   }
   ```

3. **Хелпер статус-сообщений**: новые ответы отправляются **без**
   `replyKeyboard` (reply-клавиатура с `/start` неодобренному не показывается);
   `createTextReplyMessage` для одобренного потока не трогать.
4. Кнопку «Зарегистрироваться» с рабочей обработкой появляется в T-22; в этой
   задаче достаточно отправить inline-кнопку с data `req:new` (обработчик-заглушка
   из T-18 сделает ack; нажатие станет полезным в T-22).
5. **Диспетчер `actions`, обработчики, `Searcher` — без изменений.**

## Тесты

- Табличные unit-тесты guard-функции (репозиторий на `:memory:` или фейк):
  - админ с любой строкой в `users` (в т.ч. `declined`) → allow;
  - `none/pending/declined/approved` → ожидаемый исход и ожидаемый текст;
  - статус определяется запросом к БД на каждый вызов (FR6) — проверить, что
    изменение строки между вызовами меняет исход.
- Тест: статус-ответы не содержат reply-клавиатуру; ответ одобренного — содержит.

## Критерии приёмки

- [ ] Неизвестный пользователь: ни одна команда не возвращает данные библиотеки
      (проверить `/start`, `/author X`, `/title X`, `/result`, `/get 1`).
- [ ] Pending получает статусное сообщение; поиск недоступен.
- [ ] Declined получает отказ.
- [ ] Одобренный и администратор: все команды работают как раньше.
- [ ] `go vet ./... && go test ./...` зелёные.

## Связанные документы

- ADR: [`docs/adr/0003-access-control.md`](../adr/0003-access-control.md) § 4.3
- План: [`docs/plans/access-control.md`](access-control.md)
