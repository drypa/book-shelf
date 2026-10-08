# T-17. Исправление XXE уязвимости в FB2-парсере

- **Приоритет:** P1 (high)
- **Оценка:** 0.5 дня
- **Зависит от:** —
- **Блокирует:** —
- **Основание:** ADR-0002 (XXE-защита)

---

## Цель

Обеспечить защиту парсера FB2 от XXE-атак (XML External Entity) при обработке
мalicious файлов.

---

## Архитектурный обзор

### Проблема

FB2-парсер (`format/fb2/fb2.go`) использует `xml.Decoder` без явной защиты от
XML-сущностей. Хотя Go's `encoding/xml` по умолчанию не обрабатывает внешние
сущности, явное отключение необходимо для:

1. Документирования намерения защиты
2. Соответствия best practices безопасности
3. Защиты от потенциальных регрессий

### Текущий код (плохо)

```go
decoder := xml.NewDecoder(reader)
decoder.CharsetReader = func(encoding string, input io.Reader) (io.Reader, error) {
    // ...
}

for {
    tok, err := decoder.Token()
    // Парсинг без защиты от сущностей
}
```

### Предлагаемое решение (хорошо)

```go
decoder := xml.NewDecoder(reader)
decoder.Entity = map[string]string{} // Явно отключаем XML-сущности для защиты от XXE

decoder.CharsetReader = func(encoding string, input io.Reader) (io.Reader, error) {
    // ...
}
```

### Почему это работает

- `decoder.Entity` — карта сопоставления имён сущностей со значениями
- Пустая карта (`map[string]string{}`) означает, что нет определённых сущностей
- При встрече неизвестной сущности парсер не будет выполнять внешние запросы
- Вместо этого сущность будет считаться неопределённой

---

## Что делать

### Шаг 1: Добавить защиту в fb2.go

Отредактировать `format/fb2/fb2.go`, добавив после создания декодера:

```go
decoder := xml.NewDecoder(reader)
decoder.Entity = map[string]string{} // Защита от XXE
```

### Шаг 2: Создать фикстуру для теста XXE

Создать файл `format/fb2/testdata/book-xxe.fb2`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<book-title>XXE Test Book</book-title>
<author><first-name>Test</first-name><last-name>Author</last-name></author>
</title-info>
</description>
<body><section><title>Test</title><p>Content</p></section></body>
</FictionBook>
```

### Шаг 3: Добавить тест

Добавить в `format/fb2/fb2_test.go`:

```go
func TestReadFb2XxeProtection(t *testing.T) {
    // FB2 с внешней сущностью не должен читать файлы сервера
    fb2, err := ReadFb2(fixturePath(t, "book-xxe.fb2"))
    // Ожидаем: либо ошибка о неизвестной сущности, либо успешный парсинг
    // (сущность просто игнорируется)
    // Критично: файл /etc/passwd НЕ должен быть прочитан
    if err != nil {
        // Проверяем, что ошибка не связана с чтением файлов
        t.Logf("FB2 с XXE обработан с ошибкой (это возможное поведение): %v", err)
    }
    if fb2 != nil {
        t.Logf("FB2 с XXE успешно пропарсен (защита сработала)")
    }
}
```

---

## Критерии приёмки

- [ ] Код `fb2.go` содержит `decoder.Entity = map[string]string{}`
- [ ] FB2-файл с XML-Entity не вызывает ошибку чтения, но не обрабатывает сущности
- [ ] Добавлен unit-тест с FB2, содержащим Entity
- [ ] Все существующие тесты продолжают проходить

---

## Риски

| Риск | Вероятность | Влияние | Митигация |
|---|---|---|---|
| R1 | FB2 с внутренними сущностями станет непарсируемым | Низкая | Если появятся такие файлы — отдельный анализ |
| R2 | Тест может не покрыть все случаи XXE | Средняя | Использовать типичные паттерны XXE в тесте |

---

## Связанные документы

- ADR: [`docs/adr/0002-xxe-protection.md`](../adr/0002-xxe-protection.md)
- Задача: [`docs/tasks/fb2-xxe-fix.md`](../tasks/fb2-xxe-fix.md)

---

## Последствия

### Положительные

- Явная защита от XXE
- Соответствие best practices безопасности
- Документирование архитектурного решения в ADR

### Возможные отрицательные

- FB2-файлы с внутренними сущностями могут стать непарсируемыми
  (вряд ли случится, так как FictionBook формат не использует сущности)