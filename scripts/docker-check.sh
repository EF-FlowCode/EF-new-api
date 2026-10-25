#!/usr/bin/env bash
# eflow: 所有编译 / 测试 / 检查只在 Docker 内执行，禁止在宿主机跑 go / bun / tsc / eslint。
#
# 做法：把当前工作区（git 跟踪文件 + 未跟踪但未忽略的文件）打成 tar 流，一次性灌进
# Docker 命名卷 newapi-src，之后所有步骤都在 Linux 卷上跑——不 bind mount Windows 目录
# （WSL2 跨 NTFS 读几千个源文件是主要慢点）。
#
# 持久化缓存（Docker 命名卷，跨次运行保留，删卷即清）：
#   newapi-gomod        Go 模块下载缓存           /go/pkg/mod
#   newapi-gocache      Go 编译/测试结果缓存       /root/.cache/go-build
#   newapi-bun-cache    bun 包下载缓存            /root/.bun/install/cache
#   newapi-web-modules  web/node_modules（含 tsc 增量 buildinfo 与 eslint 缓存）
#   newapi-check-mysql-data  测试 MySQL 数据目录（避免每次重新初始化）
# 同步源码时保留卷内 web/node_modules，只替换源码文件。
#
# 用法（仓库根目录，Git Bash）：
#   scripts/docker-check.sh                  # 全部：go vet + go test + go build + web
#   scripts/docker-check.sh go               # 只跑后端
#   scripts/docker-check.sh web              # 只跑前端（tsc + eslint + bun test）
#   scripts/docker-check.sh task-review-test # 任务价格核对表单定向类型/lint/浏览器验收
#   scripts/docker-check.sh pricing-editor-test # 计费规则定向测试 + 实际组件浏览器验收
#   scripts/docker-check.sh go-test ./model/ ./service/ -run Foo   # 指定包/参数
#   scripts/docker-check.sh fmt              # 容器内 gofmt/prettier 改动文件并写回工作区
#   scripts/docker-check.sh routes           # 容器内重新生成 web/src/routeTree.gen.ts 并写回工作区
#   scripts/docker-check.sh release-tests [--go] [--web] # 发布合同测试及受影响模块
#   scripts/docker-check.sh ui-image         # 构建固定版本浏览器工具镜像（需授权）
#   scripts/docker-check.sh release-ui <dir> # 本轮克隆的 Docker API/UI 门禁
#   scripts/docker-check.sh sync | down | clean-cache
# 环境变量：GO_TEST_P（go test 并行包数，默认 12）、GO_BUILD_P（编译并发，默认容器全部 CPU）
#   NEWAPI_CHECK_ID：多个工作区（git worktree）并行检查时各设一个不同值，源码卷与 web/node_modules
#   卷加该后缀互不覆盖；Go 缓存与测试 MySQL 共用（测试按包随机建库）。
set -euo pipefail

export MSYS_NO_PATHCONV=1
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

GO_IMAGE='golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba'
BUN_IMAGE='oven/bun:1.4.0@sha256:5ff609364c049b54eb0ff560ec96319729a972078ef2c755d758f0c6ef89c2d6'
MYSQL_IMAGE='mysql:8.4.8'
NETWORK='newapi-check'
MYSQL='newapi-check-mysql'
CHECK_SUFFIX="${NEWAPI_CHECK_ID:+-${NEWAPI_CHECK_ID}}"
SRC_VOL="newapi-src${CHECK_SUFFIX}"
WEB_MODULES_VOL="newapi-web-modules${CHECK_SUFFIX}"
CACHE_VOLS="newapi-gomod newapi-gocache newapi-bun-cache ${WEB_MODULES_VOL} newapi-check-mysql-data"
GO_TEST_P="${GO_TEST_P:-12}"
GO_BUILD_P="${GO_BUILD_P:-}"

log() { printf '\n==> %s\n' "$*"; }

sync_src() {
	log "sync source -> volume ${SRC_VOL}"
	docker volume create "$SRC_VOL" >/dev/null
	git ls-files -z --cached --others --exclude-standard |
		tar --null -T - --ignore-failed-read -cf - 2>/dev/null |
		docker run --rm -i -v "${SRC_VOL}:/src" alpine:3.20 sh -c '
			find /src -mindepth 1 -maxdepth 1 ! -name web -exec rm -rf {} +
			if [ -d /src/web ]; then
				find /src/web -mindepth 1 -maxdepth 1 ! -name node_modules -exec rm -rf {} +
			fi
			tar -xf - -C /src
			# main.go 用 go:embed 引用 web/dist（已被 gitignore，不同步）。vet/test/build 只需
			# 占位文件；真实前端产物由 docker build 的 bun 阶段生成，与这里无关。
			if [ ! -f /src/web/dist/index.html ]; then
				mkdir -p /src/web/dist && echo "<!doctype html><title>check</title>" > /src/web/dist/index.html
			fi'
}

ensure_network() {
	docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
}

ensure_mysql() {
	ensure_network
	if [ "$(docker inspect -f '{{.State.Running}}' "$MYSQL" 2>/dev/null)" != "true" ]; then
		log "start test MySQL (${MYSQL_IMAGE})"
		docker rm -f "$MYSQL" >/dev/null 2>&1 || true
		# 测试库每包随机新建/删除，放宽刷盘换速度；数据目录持久化免去每次初始化。
		docker run -d --name "$MYSQL" --network "$NETWORK" \
			-v newapi-check-mysql-data:/var/lib/mysql -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=newapi_test "$MYSQL_IMAGE" \
			--max-connections=2000 --innodb-flush-log-at-trx-commit=0 --sync-binlog=0 \
			--skip-log-bin --innodb-doublewrite=0 >/dev/null
	fi
	# mysqladmin ping 在初始化阶段的临时实例上也会成功，必须确认 TCP 3306 可用。
	for _ in $(seq 1 120); do
		if docker exec "$MYSQL" mysql -uroot -ptest -h127.0.0.1 -P3306 -e 'SELECT 1' >/dev/null 2>&1; then
			# 数据卷来自旧版本时不会再按 MYSQL_DATABASE 初始化，这里兜底建库。
			docker exec "$MYSQL" mysql -uroot -ptest -h127.0.0.1 -e 'CREATE DATABASE IF NOT EXISTS newapi_test' >/dev/null 2>&1
			return 0
		fi
		sleep 1
	done
	echo "test MySQL did not become ready" >&2
	docker logs --tail 30 "$MYSQL" >&2
	exit 1
}

go_run() {
	ensure_network
	local goflags=()
	[ -n "$GO_BUILD_P" ] && goflags=(-e "GOFLAGS=-p=${GO_BUILD_P}")
	docker run --rm --network "$NETWORK" \
		-e TEST_MYSQL_DSN="root:test@tcp(${MYSQL}:3306)/newapi_test?parseTime=true" \
		-e GOPROXY='https://goproxy.cn,direct' "${goflags[@]}" \
		-v "${SRC_VOL}:/build" -v newapi-gomod:/go/pkg/mod -v newapi-gocache:/root/.cache/go-build \
		-w /build "$GO_IMAGE" sh -c "$1"
}

run_go_vet() { log "go vet ./..."; go_run 'go vet ./...'; }

run_go_test() {
	ensure_mysql
	local args="${*:-./...}"
	log "go test -p ${GO_TEST_P} ${args}"
	# 各包 MySQL 用例自建随机隔离库，可安全并行；过滤 "no test files" 并保留退出码。
	go_run "{ go test -p ${GO_TEST_P} ${args} 2>&1; echo \$? > /tmp/rc; } | grep -v 'no test files'; exit \$(cat /tmp/rc)"
}

run_go_build() { log "go build ./..."; go_run 'go build ./...'; }

run_web() {
	log "web: bun install + tsc + eslint + bun test"
	docker run --rm -v "${SRC_VOL}:/src" -v "${WEB_MODULES_VOL}:/src/web/node_modules" \
		-v newapi-bun-cache:/root/.bun/install/cache -w /src/web "$BUN_IMAGE" sh -c '
			set -e
			bun install --frozen-lockfile
			bunx tsc -b
			bunx eslint --cache --cache-location node_modules/.cache/eslint/ .
			tests=$(find src -name "*.test.ts" -o -name "*.test.tsx" | sort)
			[ -z "$tests" ] || bun test $tests
		'
}

# routes：新增或改名路由文件后，在容器内重新生成 web/src/routeTree.gen.ts 并拷回工作区。
run_routes() {
	log "web: regenerate routeTree.gen.ts"
	docker run --rm -v "${SRC_VOL}:/src" -v "${WEB_MODULES_VOL}:/src/web/node_modules" \
		-v newapi-bun-cache:/root/.bun/install/cache -w /src/web "$BUN_IMAGE" sh -c '
			set -e
			bun install --frozen-lockfile >/dev/null
			bun scripts/generate-routes.mjs
		'
	docker run --rm -v "${SRC_VOL}:/src" -w /src alpine:3.20 tar -cf - web/src/routeTree.gen.ts |
		tar -xf - -C "$ROOT"
}

# 只转译隔离组件测试页，不构建整站，不连接业务服务或数据库。
run_pricing_editor_test() {
	local ui_image='new-api-release-ui:pw1.58.2'
	if ! docker image inspect "$ui_image" >/dev/null 2>&1; then
		echo "missing ${ui_image}; build with ui-image only after approval" >&2
		return 1
	fi
	log "pricing editor: focused unit tests, lint and browser fixture"
	docker run --rm -v "${SRC_VOL}:/src" -v "${WEB_MODULES_VOL}:/src/web/node_modules" \
		-v newapi-bun-cache:/root/.bun/install/cache -w /src/web "$BUN_IMAGE" sh -c '
			set -e
			bun install --frozen-lockfile
			bun test src/features/pricing/lib/billing-expr-time-rule.test.ts src/features/pricing/lib/time-pricing.test.ts src/features/system-settings/models/tiered-pricing-editor-state.test.ts
			bunx tsc --noEmit -p tsconfig.app.json
			bunx eslint src/features/pricing/lib/billing-expr.ts src/features/pricing/lib/billing-expr-time-rule.test.ts src/features/system-settings/models/tiered-pricing-editor.tsx src/features/system-settings/models/task-pricing-editor.tsx src/features/system-settings/models/task-pricing-migration-panel.tsx tests/pricing-editor.tsx
			bun build tests/pricing-editor.tsx --target browser --outdir /src/.pricing-editor-test
		'
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src "$ui_image" \
		node scripts/pricing-editor.test.cjs
}

# Isolated review form: no backend, application build, database or provider calls.
run_task_review_test() {
 local ui_image='new-api-release-ui:pw1.58.2'
 docker image inspect "$ui_image" >/dev/null 2>&1 || { echo "missing $ui_image" >&2; return 1; }
 docker run --rm -v "${SRC_VOL}:/src" -v "${WEB_MODULES_VOL}:/src/web/node_modules" \
  -v newapi-bun-cache:/root/.bun/install/cache -w /src/web "$BUN_IMAGE" sh -c '
   set -e
   bunx tsc --noEmit -p tsconfig.app.json
   bunx eslint src/features/usage-logs/components/dialogs/task-review-dialog.tsx tests/task-review.tsx
   bun build tests/task-review.tsx --target browser --outdir /src/.task-review-test
  '
 docker run --rm --network none -v "${SRC_VOL}:/src" -w /src "$ui_image" node scripts/task-review.test.cjs
}

# 复用发布备份工具中的 Python 3.11，不在宿主机安装/运行测试依赖。
# OAuth 合同测试需要固定历史版本；只传这一份 Git blob，不挂载宿主机仓库。
run_release_tests() {
	local check_go=false check_web=false
	for scope in "$@"; do
		case "$scope" in
			--go) check_go=true ;;
			--web) check_web=true ;;
			*) echo "unknown release test scope: $scope" >&2; return 2 ;;
		esac
	done
	log "release contracts: Python 3.11 in Docker"
	if ! docker image inspect new-api-backup-client:8.4.8 >/dev/null 2>&1; then
		echo "missing new-api-backup-client:8.4.8 (Python 3.11); build scripts/backup-client.Dockerfile with approval" >&2
		return 1
	fi
	git show '7114a494663d0e24f212005215f496a75fd55f01:router/api-router.go' |
		docker run --rm -i -v "${SRC_VOL}:/src" alpine:3.20 sh -c '
			mkdir -p /src/.release-test-fixtures
			cat > /src/.release-test-fixtures/oauth-router.go'
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src \
		-e RELEASE_OAUTH_BASELINE_FILE=/src/.release-test-fixtures/oauth-router.go \
		--entrypoint sh new-api-backup-client:8.4.8 -c '
			set -e
			python3.11 scripts/release.test.py
			python3.11 scripts/rolling-deploy.test.py
			python3.11 scripts/static-assets.test.py
			python3.11 scripts/backup-chain.test.py
			python3.11 -m py_compile scripts/baseline-snapshot.py scripts/release_baseline.py scripts/release_local.py scripts/release_ui.py scripts/maintenance-integration.py scripts/billing-integration.py scripts/backup-restore.py'
	# Debian dash, like the server's /bin/sh, runs the supervisor contract.
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src node:20-bookworm sh scripts/rolling-remote.test.sh
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src node:20-bookworm node --check scripts/release-ui.cjs
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src node:20-bookworm node --check scripts/task-pricing-preflight.cjs
	docker run --rm --network none -v "${SRC_VOL}:/src" -w /src node:20-bookworm node --check scripts/task-pricing-matrix.cjs
	if "$check_go"; then
		run_go_test
		log "relaykit: go test ./..."
		go_run 'cd relaykit && go test ./...'
	fi
	if "$check_web"; then run_web; fi
}

# fmt：在容器内对卷里改动过的文件执行 gofmt / prettier，再把被改写的文件拷回工作区。
# 只处理相对 HEAD 有改动或未跟踪的文件，避免全仓重排。
run_fmt() {
	local files go_files web_files
	files="$(git diff --name-only --diff-filter=ACMR HEAD; git ls-files --others --exclude-standard)"
	go_files="$(printf '%s\n' "$files" | grep -E '\.go$' || true)"
	web_files="$(printf '%s\n' "$files" | grep -E '^web/.*\.(ts|tsx|js|jsx|json|css|md)$' | sed 's#^web/##' || true)"
	if [ -z "$go_files$web_files" ]; then
		log "fmt: no changed files"
		return 0
	fi
	if [ -n "$go_files" ]; then
		log "gofmt"
		go_run "gofmt -w $(echo $go_files)"
	fi
	if [ -n "$web_files" ]; then
		log "prettier"
		# Pass paths as data: route filenames such as $chatId.tsx must never
		# be expanded as variables by the container's shell.
		printf '%s\n' "$web_files" | docker run --rm -i -v "${SRC_VOL}:/src" -v "${WEB_MODULES_VOL}:/src/web/node_modules" \
			-v newapi-bun-cache:/root/.bun/install/cache -w /src/web "$BUN_IMAGE" \
			sh -c 'bun install --frozen-lockfile >/dev/null && xargs -d "\n" bunx prettier --write'
	fi
	log "copy formatted files back"
	{
		printf '%s\n' $go_files
		printf 'web/%s\n' $web_files
	} | grep -v '^web/$' |
		docker run --rm -i -v "${SRC_VOL}:/src" -w /src alpine:3.20 sh -c 'xargs tar -cf -' |
		tar -xf - -C "$ROOT"
}

cmd="${1:-all}"
[ $# -gt 0 ] && shift
case "$cmd" in
	sync) sync_src ;;
	fmt) sync_src; run_fmt ;;
	down) docker rm -f "$MYSQL" >/dev/null 2>&1 || true ;;
	clean-cache) docker rm -f "$MYSQL" >/dev/null 2>&1 || true; docker volume rm $CACHE_VOLS "$SRC_VOL" 2>/dev/null || true ;;
	go) sync_src; run_go_vet; run_go_test; run_go_build ;;
	go-vet) sync_src; run_go_vet ;;
	go-test) sync_src; run_go_test "$@" ;;
	go-build) sync_src; run_go_build ;;
	web) sync_src; run_web ;;
	routes) sync_src; run_routes ;;
	pricing-editor-test) sync_src; run_pricing_editor_test ;;
	task-review-test) sync_src; run_task_review_test ;;
	release-tests) sync_src; run_release_tests "$@" ;;
	ui-image) docker build -f scripts/release-ui.Dockerfile -t new-api-release-ui:pw1.58.2 \
		--label "cc.e-flowcode.ui-tools=$(sha256sum scripts/release-ui.Dockerfile | cut -d ' ' -f 1)" scripts ;;
	release-ui) sync_src; "${NEW_API_PYTHON:-E:/path/python/python.exe}" scripts/release_ui.py "$@" ;;
	task-pricing-preflight) sync_src; "${NEW_API_PYTHON:-E:/path/python/python.exe}" scripts/release_ui.py "$@" --task-pricing-preflight ;;
	all) sync_src; run_go_vet; run_go_test; run_go_build; run_web ;;
	*) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
log "done: $cmd"
