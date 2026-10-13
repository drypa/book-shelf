# T-22. Callback'и: регистрация, заявка админу, решение двумя кнопками

- **Приоритет:** P0 (сценарии 1–5, FR2–FR4; закрытие H2 целиком)
- **Оценка:** 1.5 дня
- **Зависит от:** T-20, T-21
- **Блокирует:** T-23
- **Основание:** ADR-0003 § 4.4 (C1–C10)

---

## Цель

Реализовать полный поток: «Зарегистрироваться» → заявка администратору →
«Принять»/«Отклонить» → переход статуса + уведомление пользователя. Решение
идемпотентно, авторизовано по ID администратора, переживает рестарт бота.

## Что делать

1. **Новый файл `cmd/bot/callbacks.go`:**

   ```go
   const (
       cbRegister     = "req:new"
       cbApprovePrefix = "adm:approve:"
       cbDeclinePrefix = "adm:decline:"
   )
   func parseAdminAction(data string) (targetID int64, approve bool, ok bool)
   ```

2. **Обработчик `req:new`** (заменяет заглушку T-18):
   - `From.ID == adminID` → alert «Admin does not need registration» (C8);
   - `CreatePending(From.ID, FirstName, LastName, UserName, LanguageCode)`;
     создана → доставить заявку админу (`bot.Send` в чат `adminID`);
     уже была → переотправить заявку (C6) + ответ «request already sent»;
   - ошибка доставки → `slog.Error` + alert «failed to notify the admin, try again» (C7);
   - успех → ответ «Registration request sent, wait for the admin» (C5).
3. **Текст заявки админу** (FR3): `ID / Name / Last name / @username / Language`
   — только непустые поля Telegram; inline-клавиатура `[Accept] [Decline]`
   с data `adm:approve:<telegram_id>` / `adm:decline:<telegram_id>`.
4. **Обработчик `adm:*`:**
   - не `From.ID == adminID` → alert «You are not allowed to decide», БД чистая (C1);
   - `targetID == adminID` → alert «The admin cannot be blacklisted» (C9);
   - `Decide` (атомарный, T-20):
     - applied → `EditMessageText` (итог + снять клавиатуру пустым
       `InlineKeyboardMarkup`, C4) + уведомить пользователя об approved/declined
       (ошибка отправки — только `slog.Warn`); ответ админу с итогом;
     - `applied=false` → alert по `current`: already approved / already declined /
       «access revocation is out of scope» (approve→approved идемпотентен,
       decline→approved и любое→declined без изменений, C3).
5. **Неизвестный callback data** → alert «Action not supported» + `slog.Warn` (C10).
6. Каждый обработчик заканчивается `AnswerCallbackQuery` (C5).

## Тесты

- `parseAdminAction`: валидные `adm:approve:123` / `adm:decline:123`, мусор,
  не-число, пустой хвост, чужой префикс.
- Конструктор текста заявки: все поля; без last name/username/language —
  соответствующие строки отсутствуют.
- Идемпотентность на репозитории (`:memory:`): второй `adm:approve` не меняет
  статус; `adm:approve` после decline не меняет.
- (По возможности) табличный тест обработчика с фейковым «отправителем»
  сообщений: выборка alert/итог по парам (current, action).

## Критерии приёмки (ручная проверка, см. также T-23)

- [ ] Новый пользователь: кнопка → у админа заявка со всеми полями и двумя кнопками.
- [ ] Из pending повторная кнопка не создаёт дубль; админ не получает вторую «активную» заявку.
- [ ] «Принять»: пользователь сразу может `/start → /author → /get 1`; сообщение
      заявки погашено, кнопки сняты.
- [ ] «Отклонить»: ни доступа, ни повторной заявки.
- [ ] Чужое нажатие на кнопки решения (тестовый второй аккаунт, куда переслали
      сообщение) — alert, статус не меняется.
- [ ] Двойное нажатие/устаревшая кнопка — понятный ответ, статус не меняется.
- [ ] Рестарт бота между заявкой и решением — кнопки старой заявки работают.
- [ ] `go vet ./... && go test ./...` зелёные.

## Риски

| Риск | Митигация |
|---|---|
| Админ не написал боту → доставка заявки падает | RR1 ADR: alert + переотправка; README-шаг в T-23 |
| `EditMessageText` упал (сеть) | решение уже в БД; кнопки гасятся C3 при повторе |

## Связанные документы

- ADR: [`docs/adr/0003-access-control.md`](../adr/0003-access-control.md) § 4.4
- План: [`docs/plans/access-control.md`](access-control.md)
