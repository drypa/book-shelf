#!/usr/bin/env bash
# Разовый запуск контейнера сканирования. Аргумент (каталог библиотеки) задан
# в compose как command сервиса scan — здесь он не дублируется.
#
# ВНИМАНИЕ: контейнер scan монтирует каталог библиотеки НА ЗАПИСЬ и создаёт
# рядом с архивами *.zip.json, меняя владельца файлов на хосте.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/common.sh
. ./scripts/common.sh

preflight
# --rm        : удалить одноразовый контейнер после завершения
# --no-deps   : не поднимать другие сервисы, даже если появится depends_on
# -T          : без псевдо-TTY, чтобы логи оставались машинно-читаемыми
# Код возврата контейнера пробрасывается автоматически (set -e).
compose --profile "$COMPOSE_PROFILES" run --rm --no-deps -T scan

echo "Сканирование завершено. Следующий шаг: ./db-create.sh"