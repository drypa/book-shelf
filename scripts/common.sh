#!/usr/bin/env bash
# Общие хелперы для build.sh / up.sh / down.sh / scan.sh / db-create.sh.
# Источник: build.sh up.sh down.sh scan.sh db-create.sh

# shellcheck disable=SC2034  # используется в build.sh / scan.sh / db-create.sh, которые нас подключают
COMPOSE_PROFILES="tools"

compose() { docker compose "$@"; }

require_repo_layout() {
  if [ ! -f docker-compose.yml ]; then
    echo "ОШИБКА: docker-compose.yml не найден в $(pwd)." >&2
    echo "Запускайте скрипты из корня репозитория." >&2
    exit 1
  fi
}

require_env_file() {
  if [ ! -f .env ]; then
    echo "ОШИБКА: файл .env не найден." >&2
    echo "Создайте его из шаблона:  cp .env.example .env" >&2
    echo "и заполните как минимум BOT_TOKEN." >&2
    exit 1
  fi
}

# -q (quiet) печатает ТОЛЬКО ошибки интерполяции/синтаксиса compose и ничего успешного.
# Это важно: `docker compose config` без -q напечатал бы BOT_TOKEN в терминал.
preflight() {
  require_repo_layout
  require_env_file
  compose config -q
}