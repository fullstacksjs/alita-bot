#!/usr/bin/env bash
# Acceptance check for the single supported production container.
#
# It starts the image against an empty data volume, waits for the container
# health probe, then recreates the container against the same volume and proves
# the SQLite database survived: the first start applies the embedded migrations
# and the second start skips all of them.
#
# Telegram is stubbed. gotgbot validates the token with getMe before the health
# endpoint is served, so a fake api.telegram.org runs on the container network
# and its CA is mounted over the runtime CA bundle. Nothing here reaches the
# real Telegram API, so no bot token secret is needed.
#
# Usage: scripts/docker-acceptance/run.sh [image]

set -euo pipefail

IMAGE="${1:-alita-acceptance:latest}"
NETWORK="alita-acceptance-net"
VOLUME="alita-acceptance-data"
BOT_CONTAINER="alita-acceptance"
API_CONTAINER="alita-acceptance-telegram"
RUNTIME_UID=65532

workdir="$(mktemp -d)"

cleanup() {
  docker rm -f "$BOT_CONTAINER" "$API_CONTAINER" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "--- bot container logs ---" >&2
  docker logs "$BOT_CONTAINER" >&2 2>&1 || true
  exit 1
}

echo "==> Generating a CA for the fake Telegram API"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout "$workdir/api.key" -out "$workdir/api.crt" \
  -subj "/CN=api.telegram.org" \
  -addext "subjectAltName=DNS:api.telegram.org" >/dev/null 2>&1
chmod 644 "$workdir/api.key" "$workdir/api.crt"

cat >"$workdir/api.conf" <<'NGINX'
server {
    listen 443 ssl;
    server_name api.telegram.org;

    ssl_certificate     /certs/api.crt;
    ssl_certificate_key /certs/api.key;

    default_type application/json;

    location ~ ^/bot[^/]+/getMe$ {
        return 200 '{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Acceptance","username":"acceptance_bot","can_join_groups":true,"can_read_all_group_messages":false,"supports_inline_queries":false}}';
    }

    location ~ ^/bot[^/]+/getUpdates$ {
        return 200 '{"ok":true,"result":[]}';
    }

    # deleteWebhook, setMyCommands, and anything else the bootstrap calls.
    location / {
        return 200 '{"ok":true,"result":true}';
    }
}
NGINX

echo "==> Starting the stub Telegram API"
docker network create "$NETWORK" >/dev/null
docker volume create "$VOLUME" >/dev/null
docker run -d --name "$API_CONTAINER" \
  --network "$NETWORK" --network-alias api.telegram.org \
  -v "$workdir/api.crt:/certs/api.crt:ro" \
  -v "$workdir/api.key:/certs/api.key:ro" \
  -v "$workdir/api.conf:/etc/nginx/conf.d/default.conf:ro" \
  nginx:alpine >/dev/null

start_bot() {
  docker run -d --name "$BOT_CONTAINER" \
    --network "$NETWORK" \
    -e BOT_TOKEN="123456789:acceptance-token" \
    -e OWNER_ID=1 \
    -e LOG_LEVEL=info \
    -v "$VOLUME:/data" \
    -v "$workdir/api.crt:/etc/ssl/certs/ca-certificates.crt:ro" \
    "$IMAGE" >/dev/null
}

wait_for_health() {
  local attempt
  for attempt in $(seq 1 60); do
    if docker exec "$BOT_CONTAINER" /app/alita_robot --health >/dev/null 2>&1; then
      echo "==> Healthy after ${attempt} attempt(s)"
      return 0
    fi
    if [ "$(docker inspect -f '{{.State.Running}}' "$BOT_CONTAINER")" != "true" ]; then
      fail "container exited before becoming healthy"
    fi
    sleep 2
  done
  fail "container never reported healthy"
}

echo "==> First start against an empty volume"
start_bot
wait_for_health

actual_uid="$(docker exec "$BOT_CONTAINER" id -u)"
[ "$actual_uid" = "$RUNTIME_UID" ] || fail "runtime user is uid ${actual_uid}, expected ${RUNTIME_UID}"
docker exec "$BOT_CONTAINER" test -s /data/alita.db || fail "the database was not created under /data"
docker exec "$BOT_CONTAINER" test -w /data/alita.db || fail "the database is not writable by the runtime user"

first_run="$(docker logs "$BOT_CONTAINER" 2>&1 | grep -o 'Migration complete - Applied: [0-9]*, Skipped: [0-9]*' | tail -1)"
applied="${first_run##*Applied: }"
applied="${applied%%,*}"
[ -n "$applied" ] && [ "$applied" -gt 0 ] || fail "first start did not apply the embedded migrations (${first_run:-no migration summary})"
echo "==> First start applied ${applied} migration(s)"

echo "==> Recreating the container against the same volume"
docker rm -f "$BOT_CONTAINER" >/dev/null
start_bot
wait_for_health

second_run="$(docker logs "$BOT_CONTAINER" 2>&1 | grep -o 'Migration complete - Applied: [0-9]*, Skipped: [0-9]*' | tail -1)"
[ "$second_run" = "Migration complete - Applied: 0, Skipped: ${applied}" ] ||
  fail "second start did not reuse the persisted database (${second_run:-no migration summary})"

echo "==> Container starts clean, becomes healthy, persists SQLite, and stays healthy after recreation"
