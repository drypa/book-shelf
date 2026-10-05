#!/usr/bin/env bash
# Сборка образов всех трёх сервисов: bot, scan, db-create.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/common.sh
. ./scripts/common.sh

preflight
# --profile tools ОБЯЗАТЕЛЕН: docker compose build без профиля НЕ собирает
# сервисы scan и db-create (проверено на Compose v5.1.0).
compose --profile "$COMPOSE_PROFILES" build
compose --profile "$COMPOSE_PROFILES" config -q

echo "Готово: образы book-shelf-bot, book-shelf-scan, book-shelf-db-create собраны."
echo "Запуск бота:            ./up.sh"
echo "Пополнение библиотеки:  ./scan.sh && ./db-create.sh"