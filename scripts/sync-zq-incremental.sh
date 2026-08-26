#!/usr/bin/env bash
set -euo pipefail

# Backfill or incrementally sync ZQ accounts/assets/invitations/voices into the currently
# running Canvas SQLite volume without recreating the backend or web container.
# Source credentials are read from a local or SSH-reachable zq-backend container
# into a short-lived 0600 file and are removed by the EXIT trap.

MODE=${1:-once}
ZQ_SOURCE_MODE=${ZQ_SOURCE_MODE:-auto}
ZQ_SSH_ALIAS=${ZQ_SSH_ALIAS:-ai}
ZQ_REMOTE_BACKEND=${ZQ_REMOTE_BACKEND:-zq-backend}
ZQ_REMOTE_PUBLIC_NETWORK=${ZQ_REMOTE_PUBLIC_NETWORK:-zq-platform-network}
ZQ_REMOTE_DATA_NETWORK=${ZQ_REMOTE_DATA_NETWORK:-zq-platform-data-network}
CANVAS_MIGRATION_IMAGE=${CANVAS_MIGRATION_IMAGE:-open-ai-canvas-backend:zq-migration}
CANVAS_BACKUP_IMAGE=${CANVAS_BACKUP_IMAGE:-python:3.12-slim}
CANVAS_DATA_VOLUME=${CANVAS_DATA_VOLUME:-open-ai-canvas_backend-data}
CANVAS_HEALTH_URL=${CANVAS_HEALTH_URL:-http://127.0.0.1:5173/api/health}
OVERLAP=${OVERLAP:-5m}
INTERVAL=${INTERVAL:-5s}
BACKUP_BEFORE_SYNC=${BACKUP_BEFORE_SYNC:-1}
SYNC_PLATFORM_STORAGE=${SYNC_PLATFORM_STORAGE:-1}
PLATFORM_STORAGE_PATH_PREFIX=${PLATFORM_STORAGE_PATH_PREFIX:-canvas}
PLATFORM_STORAGE_ACTOR_USER_ID=${PLATFORM_STORAGE_ACTOR_USER_ID:-}
REPLACE_PLATFORM_STORAGE=${REPLACE_PLATFORM_STORAGE:-0}

case "$MODE" in
  full|once|watch) ;;
  *) printf 'usage: %s [full|once|watch]\n' "$0" >&2; exit 2 ;;
esac
case "$ZQ_SOURCE_MODE" in
  auto|local|ssh) ;;
  *) printf 'ZQ_SOURCE_MODE must be auto, local, or ssh\n' >&2; exit 2 ;;
esac
case "$SYNC_PLATFORM_STORAGE:$REPLACE_PLATFORM_STORAGE" in
  0:0|0:1|1:0|1:1) ;;
  *) printf 'SYNC_PLATFORM_STORAGE and REPLACE_PLATFORM_STORAGE must be 0 or 1\n' >&2; exit 2 ;;
esac

for command in docker python3 curl; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 2; }
done
docker image inspect "$CANVAS_MIGRATION_IMAGE" >/dev/null
docker volume inspect "$CANVAS_DATA_VOLUME" >/dev/null

if [ "$ZQ_SOURCE_MODE" = "auto" ]; then
  if docker inspect "$ZQ_REMOTE_BACKEND" >/dev/null 2>&1; then
    ZQ_SOURCE_MODE=local
  else
    ZQ_SOURCE_MODE=ssh
  fi
fi
if [ "$ZQ_SOURCE_MODE" = "ssh" ]; then
  command -v ssh >/dev/null 2>&1 || { printf 'missing command: ssh\n' >&2; exit 2; }
else
  docker inspect "$ZQ_REMOTE_BACKEND" >/dev/null
  docker network inspect "$ZQ_REMOTE_DATA_NETWORK" >/dev/null
fi

lock_dir=${TMPDIR:-/tmp}/open-ai-canvas-zq-sync.lock
if ! mkdir "$lock_dir" 2>/dev/null; then
  printf 'another ZQ sync is already running: %s\n' "$lock_dir" >&2
  exit 3
fi

raw_env=$(mktemp "${TMPDIR:-/tmp}/zq-source-env.XXXXXX")
source_env=$(mktemp "${TMPDIR:-/tmp}/zq-source-runtime.XXXXXX")
ssh_socket=$(mktemp -u "${TMPDIR:-/tmp}/zq-sync-ssh.XXXXXX")
relay_container="zq-db-sync-relay-$$"
relay_script="/tmp/${relay_container}.py"
ssh_started=0
relay_started=0

unlink_file() {
  python3 - "$1" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
if p.exists() or p.is_socket():
    p.unlink()
PY
}

cleanup() {
  rc=$?
  set +e
  if [ "$ssh_started" -eq 1 ]; then
    ssh -S "$ssh_socket" -O exit "$ZQ_SSH_ALIAS" >/dev/null 2>&1
  fi
  if [ "$relay_started" -eq 1 ]; then
    ssh "$ZQ_SSH_ALIAS" "docker rm -f '$relay_container' >/dev/null 2>&1 || true; unlink '$relay_script' >/dev/null 2>&1 || true" >/dev/null 2>&1
  fi
  unlink_file "$raw_env"
  unlink_file "$source_env"
  unlink_file "$ssh_socket"
  rmdir "$lock_dir" >/dev/null 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT INT TERM

pre_health=$(curl -fsS --max-time 3 "$CANVAS_HEALTH_URL")
printf 'CANVAS_PRE_SYNC_HEALTH=%s\n' "$pre_health"
printf 'ZQ_SYNC_SCOPE=accounts,assets,invitation_codes,invitation_usages,voice_profiles omitted=messages,generation_tasks,notifications source_mode=%s\n' "$ZQ_SOURCE_MODE"

source_host=postgres
source_port=5432
if [ "$ZQ_SOURCE_MODE" = "ssh" ]; then
  remote_image=$(ssh "$ZQ_SSH_ALIAS" "docker inspect '$ZQ_REMOTE_BACKEND' --format '{{.Config.Image}}'")
  remote_port=$(ssh "$ZQ_SSH_ALIAS" 'for p in $(seq 25433 25999); do if ! ss -H -lnt 2>/dev/null | grep -qE ":${p}[[:space:]]"; then echo "$p"; exit 0; fi; done; exit 1')
  local_port=$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(('0.0.0.0', 0))
print(s.getsockname()[1])
s.close()
PY
  )

  ssh "$ZQ_SSH_ALIAS" "cat > '$relay_script'" <<'PY'
import asyncio

async def pipe(reader, writer):
    try:
        while True:
            chunk = await reader.read(65536)
            if not chunk:
                break
            writer.write(chunk)
            await writer.drain()
    finally:
        try:
            writer.close()
            await writer.wait_closed()
        except Exception:
            pass

async def handle(client_reader, client_writer):
    server_reader, server_writer = await asyncio.open_connection('postgres', 5432)
    await asyncio.gather(
        pipe(client_reader, server_writer),
        pipe(server_reader, client_writer),
        return_exceptions=True,
    )

async def main():
    server = await asyncio.start_server(handle, '0.0.0.0', 15432)
    async with server:
        await server.serve_forever()

asyncio.run(main())
PY

  ssh "$ZQ_SSH_ALIAS" \
    "docker run -d --name '$relay_container' --restart no --network '$ZQ_REMOTE_PUBLIC_NETWORK' -p '127.0.0.1:${remote_port}:15432' -v '$relay_script:/relay.py:ro' --entrypoint python '$remote_image' -u /relay.py >/dev/null && docker network connect '$ZQ_REMOTE_DATA_NETWORK' '$relay_container'"
  relay_started=1

  ssh -g -M -S "$ssh_socket" -fnNT -o ExitOnForwardFailure=yes \
    -L "0.0.0.0:${local_port}:127.0.0.1:${remote_port}" "$ZQ_SSH_ALIAS"
  ssh_started=1

  ssh "$ZQ_SSH_ALIAS" "docker inspect '$ZQ_REMOTE_BACKEND' --format '{{range .Config.Env}}{{println .}}{{end}}'" > "$raw_env"
  source_host=host.docker.internal
  source_port=$local_port
else
  docker inspect "$ZQ_REMOTE_BACKEND" --format '{{range .Config.Env}}{{println .}}{{end}}' > "$raw_env"
fi
chmod 600 "$raw_env"
python3 - "$raw_env" "$source_env" "$source_host" "$source_port" <<'PY'
from pathlib import Path
from urllib.parse import quote
import sys

values = {}
for line in Path(sys.argv[1]).read_text().splitlines():
    if '=' in line:
        key, value = line.split('=', 1)
        values[key] = value
required = ['DB_USER', 'DB_PASSWORD', 'DB_NAME']
missing = [key for key in required if not values.get(key)]
if missing:
    raise SystemExit('missing remote database settings: ' + ','.join(missing))
host = sys.argv[3]
port = sys.argv[4]
dsn = 'postgresql://%s:%s@%s:%s/%s?sslmode=disable' % (
    quote(values['DB_USER'], safe=''),
    quote(values['DB_PASSWORD'], safe=''),
    host,
    port,
    quote(values['DB_NAME'], safe=''),
)
lines = ['DATABASE_URL=' + dsn]
for key in [
    'QCLOUD_COS_BUCKET',
    'QCLOUD_COS_REGION',
    'QCLOUD_COS_DOMAIN',
    'QCLOUD_COS_INTERNAL_ENDPOINT',
    'QCLOUD_COS_PUBLIC_READ',
    'QCLOUD_COS_SECRET_ID',
    'QCLOUD_COS_ACCESS_KEY',
    'QCLOUD_COS_SECRET_KEY',
]:
    if values.get(key):
        lines.append(key + '=' + values[key])
Path(sys.argv[2]).write_text('\n'.join(lines) + '\n')
Path(sys.argv[1]).unlink()
PY
chmod 600 "$source_env"

if [ "$BACKUP_BEFORE_SYNC" = "1" ]; then
  backup_name="open_ai_canvas-before-zq-incremental-$(date +%Y%m%d-%H%M%S).db"
  docker run --rm -i --user 0 -v "$CANVAS_DATA_VOLUME:/data" "$CANVAS_BACKUP_IMAGE" python - "$backup_name" <<'PY'
import sqlite3
import sys
name = sys.argv[1]
source = sqlite3.connect('file:/data/open_ai_canvas.db?mode=ro', uri=True, timeout=30)
target = sqlite3.connect('/data/backups/' + name)
with target:
    source.backup(target, pages=256, sleep=0.05)
result = target.execute('PRAGMA integrity_check').fetchone()[0]
target.close()
source.close()
if result != 'ok':
    raise SystemExit('backup integrity check failed: ' + result)
print('CANVAS_ONLINE_BACKUP=/data/backups/' + name + ' integrity=ok')
PY
fi

common_args=(
  --source-env /run/secrets/zq.env
  --target-driver sqlite
  --target-dsn '/data/open_ai_canvas.db?_busy_timeout=30000&_journal_mode=WAL&_foreign_keys=on&_synchronous=NORMAL'
  --data-dir /data
)
docker_args=(
  --rm
  # The 0600 source env remains unreadable to the image's unprivileged UID on a
  # bind mount. The one-shot migration process runs as root without publishing
  # ports; the temp file is read-only and removed by the EXIT trap.
  --user 0
  -v "$source_env:/run/secrets/zq.env:ro"
  -v "$CANVAS_DATA_VOLUME:/data"
)
if [ "$ZQ_SOURCE_MODE" = "local" ]; then
  docker_args+=(--network "$ZQ_REMOTE_DATA_NETWORK")
fi
docker_args+=("$CANVAS_MIGRATION_IMAGE" migrate-zq-studio)

if [ "$SYNC_PLATFORM_STORAGE" = "1" ]; then
  storage_args=("${common_args[@]}" --storage-path-prefix "$PLATFORM_STORAGE_PATH_PREFIX")
  if [ -n "$PLATFORM_STORAGE_ACTOR_USER_ID" ]; then
    storage_args+=(--storage-actor-user-id "$PLATFORM_STORAGE_ACTOR_USER_ID")
  fi
  if [ "$REPLACE_PLATFORM_STORAGE" = "1" ]; then
    storage_args+=(--replace-platform-storage)
  fi
  docker run "${docker_args[@]}" storage "${storage_args[@]}"
fi

if [ "$MODE" = "full" ]; then
  docker run "${docker_args[@]}" backfill "${common_args[@]}"
  docker run "${docker_args[@]}" verify "${common_args[@]}"
elif [ "$MODE" = "once" ]; then
  docker run "${docker_args[@]}" follow "${common_args[@]}" --once --overlap "$OVERLAP"
  docker run "${docker_args[@]}" verify "${common_args[@]}"
else
  printf 'ZQ_INCREMENTAL_WATCH interval=%s overlap=%s\n' "$INTERVAL" "$OVERLAP"
  docker run "${docker_args[@]}" follow "${common_args[@]}" --interval "$INTERVAL" --overlap "$OVERLAP"
fi

post_health=$(curl -fsS --max-time 3 "$CANVAS_HEALTH_URL")
printf 'CANVAS_POST_SYNC_HEALTH=%s\n' "$post_health"
printf 'ZQ_INCREMENTAL_SYNC mode=%s result=passed\n' "$MODE"
