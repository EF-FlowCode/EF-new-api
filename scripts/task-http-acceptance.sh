#!/usr/bin/env bash
# Requires explicit build/service authorization. Uses only synthetic data.
set -euo pipefail
export MSYS_NO_PATHCONV=1
cd "$(git rev-parse --show-toplevel)"
image="${1:?usage: task-http-acceptance.sh <already-built-image>}"
prefix="newapi-task-http-$(date +%Y%m%d-%H%M%S)"
evidence_dir=".releases/validation/${prefix}"
mkdir -p "$evidence_dir"
# Freeze the actual image; a mutable tag cannot select a different worker later.
image="$(docker image inspect --format '{{.Id}}' "$image")"
docker image inspect "$image" > "$evidence_dir/image.json"
# MSYS defaults to a binary '*' marker; pin bytes and normalize only the marker.
sha256sum --binary scripts/task-http-acceptance.sh scripts/task-http-acceptance.cjs scripts/task-http-protocol.cjs scripts/task-http-upstream.cjs | sed 's/ \*/  /' > "$evidence_dir/scripts.sha256"
script_sha="$(sha256sum "$evidence_dir/scripts.sha256" | cut -d ' ' -f 1)"
printf '%s\n' "$prefix" > .ai-governance/workspace/task-http-current-prefix.txt
scripts/docker-check.sh sync
network="$prefix-net"
volume="$prefix-evidence"
names=()
cleanup() {
  rc=$?
  trap - EXIT
  for name in "${names[@]}"; do
    docker logs "$name" > "$evidence_dir/$name.log" 2>&1 || true
  done
  for ((i=${#names[@]}-1; i>=0; i--)); do docker stop -t 15 "${names[i]}" > /dev/null 2>&1 || true; done
  docker run --rm -v "$volume:/evidence" alpine:3.20 tar -C /evidence -cf - . | tar -C "$evidence_dir" -xf - || true
  docker inspect --format '{{.Name}} {{.State.Status}}' "${names[@]}" > "$evidence_dir/container-final-state.txt" || true
  printf 'exit=%s\nevidence=%s\n' "$rc" "$evidence_dir"
  exit "$rc"
}
subnet_args=()
if [ -n "${NEW_API_TASK_TEST_SUBNET:-}" ]; then subnet_args=(--subnet "$NEW_API_TASK_TEST_SUBNET"); fi
docker network create --internal "${subnet_args[@]}" "$network" > /dev/null
docker volume create "$volume" > /dev/null
trap cleanup EXIT
names+=("$prefix-mysql")
docker run -d --name "$prefix-mysql" --network "$network" --network-alias mysql -e MYSQL_ROOT_PASSWORD=TaskHttpSyntheticOnly -e MYSQL_DATABASE=task_http -v "$prefix-mysql-data:/var/lib/mysql" mysql:8.4.8 > /dev/null
names+=("$prefix-redis")
docker run -d --name "$prefix-redis" --network "$network" --network-alias redis redis:7-alpine > /dev/null
names+=("$prefix-upstream")
docker run -d --name "$prefix-upstream" --network "$network" --network-alias upstream -v newapi-src:/src:ro new-api-release-ui:pw1.58.2 node /src/scripts/task-http-upstream.cjs > /dev/null
ready=false
for _ in $(seq 1 90); do
  if docker exec -e MYSQL_PWD=TaskHttpSyntheticOnly "$prefix-mysql" mysql -uroot -h127.0.0.1 -e 'SELECT 1' > /dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
$ready || { echo 'isolated MySQL not ready'; exit 1; }
envs=(-e 'SQL_DSN=root:TaskHttpSyntheticOnly@tcp(mysql:3306)/task_http?charset=utf8mb4&parseTime=True&loc=Local' -e REDIS_CONN_STRING=redis://redis:6379 -e SESSION_SECRET=task-http-synthetic-session-20261004 -e EFLOW_AUTH_SECRET=task-http-synthetic-auth-20261004 -e SESSION_COOKIE_SECURE=false -e BATCH_UPDATE_ENABLED=false -e SYNC_FREQUENCY=1 -e CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED=false -e SQL_MAX_OPEN_CONNS=15 -e SQL_MAX_IDLE_CONNS=3)
names+=("$prefix-migrate")
docker run --name "$prefix-migrate" --network "$network" "${envs[@]}" -e DEPLOYMENT_ROLE=migrate -e NODE_NAME="$prefix-migrate" "$image" > "$evidence_dir/migrate.log" 2>&1
for role in web worker; do
  names+=("$prefix-$role")
  docker run -d --name "$prefix-$role" --network "$network" --network-alias "$role" "${envs[@]}" -e DEPLOYMENT_ROLE="$role" -e NODE_NAME="$prefix-$role" -v "$prefix-$role-data:/data" "$image" > /dev/null
done
check() {
  docker run --rm --network "$network" -v newapi-src:/src:ro -v "$volume:/evidence" -e TASK_HTTP_IMAGE="$image" -e TASK_HTTP_SCRIPT_SHA="$script_sha" new-api-release-ui:pw1.58.2 node /src/scripts/task-http-acceptance.cjs "$1"
}
check before-restart
docker stop -t 15 "$prefix-web" "$prefix-worker" > /dev/null
docker start -a "$prefix-migrate" >> "$evidence_dir/migrate.log" 2>&1
docker start "$prefix-web" "$prefix-worker" > /dev/null
check after-restart
