#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Использование: $0 <owner/repo> [commit_message]"
  echo "Пример: GH_TOKEN=... $0 myuser/diatune-safe-private 'Первичная загрузка приватного репозитория с бинарником'"
  exit 1
fi

REPO="$1"
COMMIT_MESSAGE="${2:-Initial private import with binary}"

if [[ -z "${GH_TOKEN:-}" ]]; then
  echo "Нужно передать GH_TOKEN в переменных окружения."
  exit 1
fi

if [[ ! -x "./tools/gh" ]]; then
  echo "Не найден исполняемый файл ./tools/gh."
  exit 1
fi

GH="./tools/gh"
TMP_UPLOAD_DIR="./.publish_tmp"
mkdir -p "$TMP_UPLOAD_DIR"

# Create private repo if it doesn't exist
if ! "$GH" api "repos/${REPO}" >/dev/null 2>&1; then
  OWNER="${REPO%%/*}"
  NAME="${REPO##*/}"
  "$GH" api -X POST "user/repos" -f name="$NAME" -F private=true >/dev/null || {
    echo "Не удалось создать репозиторий через user/repos. Для репозитория организации создайте его вручную и повторите запуск."
    exit 1
  }
  echo "Создан приватный репозиторий: ${REPO}"
else
  echo "Репозиторий уже существует: ${REPO}"
fi

# Upload all files except local caches/secrets/runtime dirs
mapfile -d '' FILES < <(find . -type f \
  ! -path './.git/*' \
  ! -path './.venv/*' \
  ! -path './.uv/*' \
  ! -path './.go/*' \
  ! -path './.publish_tmp/*' \
  ! -path './.gocache/*' \
  ! -path './.gomodcache/*' \
  ! -path './.gotmp/*' \
  ! -path './.npm-global/*' \
  ! -path './.pytest_cache/*' \
  ! -path './data/*' \
  ! -path './release/diatune-safe-linux-amd64' \
  ! -name '.bash_history' \
  ! -name '.tmp*' \
  ! -name 'tmp_*' \
  ! -path './uv' \
  ! -path './tools/gh' \
  ! -name '.env' \
  -print0)

TOTAL="${#FILES[@]}"
COUNT=0
for file in "${FILES[@]}"; do
  rel="${file#./}"
  existing_sha="$("$GH" api "repos/${REPO}/contents/${rel}" --jq .sha 2>/dev/null || true)"
  if [[ ! "$existing_sha" =~ ^[0-9a-f]{40}$ ]]; then
    existing_sha=""
  fi
  payload="$(mktemp -p "$TMP_UPLOAD_DIR" payload.XXXXXX.json)"
  {
    printf '{"message":"%s","content":"' "${COMMIT_MESSAGE//\"/\'}"
    if base64 --help 2>/dev/null | grep -q -- '-w'; then
      base64 -w 0 "$file"
    else
      base64 "$file" | tr -d '\n'
    fi
    if [[ -n "$existing_sha" && "$existing_sha" != "null" ]]; then
      printf '","sha":"%s"}' "$existing_sha"
    else
      printf '"}'
    fi
  } > "$payload"

  if ! "$GH" api -X PUT "repos/${REPO}/contents/${rel}" --input "$payload" >/dev/null; then
    echo "Ошибка загрузки файла: ${rel}"
    echo "Размер payload: $(wc -c < "$payload") байт"
    rm -f "$payload"
    exit 1
  fi
  rm -f "$payload"

  COUNT=$((COUNT+1))
  if (( COUNT % 20 == 0 )); then
    echo "Загружено ${COUNT}/${TOTAL} файлов..."
  fi
done

echo "Готово. Загружено ${COUNT} файлов в https://github.com/${REPO}"
