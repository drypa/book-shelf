#!/usr/bin/env bash
# Поднимает ТОЛЬКО бота (сервисы scan и db-create скрыты профилем "tools").
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/common.sh
. ./scripts/common.sh

preflight
compose up -d

echo "Статус:  docker compose ps"
echo "Логи:    docker compose logs -f bot"