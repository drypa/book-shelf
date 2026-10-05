#!/usr/bin/env bash
# Останавливает бота и удаляет контейнеры/сеть проекта (docker compose down).
# Данные не теряются: библиотека и база — bind-mounts на хосте, у compose
# нет volumes; удаляются только контейнеры, сеть и одноразовые контейнеры tools.
# Нужен --profile tools? Нет: scan и db-create запускаются через `run --rm`,
# они не остаются в проекте, а `down` снимает все сервисы проекта.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/common.sh
. ./scripts/common.sh

preflight
compose down

echo "Остановлено. Запуск бота: ./up.sh"
