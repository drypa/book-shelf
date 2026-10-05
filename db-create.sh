#!/usr/bin/env bash
# Разовый запуск контейнера наполнения базы из *.zip.json.
# ВНИМАНИЕ: повторный запуск ДУБЛИРУЕТ строки (см. README, раздел «Переиндексация»).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/common.sh
. ./scripts/common.sh

preflight
compose --profile "$COMPOSE_PROFILES" run --rm --no-deps -T db-create

echo "База наполнена. Если бот запущен — перезапустите его: ./up.sh"