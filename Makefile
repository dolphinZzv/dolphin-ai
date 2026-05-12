.PHONY: all build build-prod test test-all test-integration generate coverage coverage-html clean \
        check ui-build start stop prod

# ─── 门禁检查（启动前）────────────────────────────────────

# 启动前门禁：vet + 编译 + 测试 + 验证 + 前端类型检查
check:
	@echo "=== 门禁检查: go vet ==="
	go vet ./...
	@echo "=== 门禁检查: go build ==="
	go build -race -o /dev/null ./cmd/server/
	@echo "=== 门禁检查: Go 测试 ==="
	go test -race -count=1 ./internal/...
	@echo "=== 门禁检查: 验证 ==="
	go test -v -count=1 ./verif/
	@echo "=== 门禁检查: 前端类型检查 ==="
	cd ui && npx tsc --noEmit
	@echo "=== 门禁检查: 全部通过 ==="

# ─── 前端构建 ──────────────────────────────────────────────

ui-build:
	@echo "=== 构建前端 ==="
	cd ui && npm run build

# ─── 后端构建 ──────────────────────────────────────────────

build:
	go build -race -o bin/chick ./cmd/server/

# ─── 启动 / 停止 ────────────────────────────────────────────

# 启动前门禁 + 构建前端 + 构建后端 + 启动 + 启动后健康检查
start: check ui-build build
	@echo "=== 启动应用 ==="
	CHICK_JWT_SECRET=$${CHICK_JWT_SECRET:-chick-dev-secret-key-2024} ./bin/chick &>/tmp/chick-server.log &
	@echo "  PID: $$!"
	@sleep 3
	@echo "=== 启动后检查: 健康端点 ==="
	@status=$$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health 2>/dev/null); \
	if [ "$$status" = "200" ]; then \
		echo "  ✅ 健康检查通过 (HTTP $$status)"; \
	else \
		echo "  ❌ 健康检查失败 (HTTP $$status)"; \
		exit 1; \
	fi
	@echo "=== 启动后检查: 页面 ==="
	@status=$$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/ 2>/dev/null); \
	if [ "$$status" = "200" ]; then \
		echo "  ✅ 页面返回 200"; \
	else \
		echo "  ❌ 页面返回 $$status"; \
		exit 1; \
	fi
	@echo "=== 启动完成: http://0.0.0.0:8080 ==="

stop:
	@echo "=== 停止应用 ==="
	@pkill -f "bin/chick" 2>/dev/null || true
	@echo "  ✅ 已停止"

# ─── 测试 ──────────────────────────────────────────────────

test:
	go test -race -count=1 ./internal/auth/ ./internal/config/ ./internal/events/ ./internal/matching/ ./internal/mcp/ ./internal/notifications/ ./internal/repository/gorm/ ./internal/server/ ./internal/service/

test-all:
	go test -race -count=1 ./internal/...

test-integration:
	go test -tags=integration -race -count=1 ./internal/...

# ─── 代码生成 ──────────────────────────────────────────────

generate:
	cd ui && npx graphql-codegen
	go run github.com/99designs/gqlgen generate

# ─── 覆盖率 ────────────────────────────────────────────────

coverage:
	go test -count=1 -coverprofile=coverage.out ./internal/auth/ ./internal/config/ ./internal/events/ ./internal/matching/ ./internal/mcp/ ./internal/notifications/ ./internal/repository/gorm/ ./internal/server/ ./internal/service/
	go tool cover -func=coverage.out | grep total

coverage-html:
	go test -count=1 -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out

# ─── 生产部署 ──────────────────────────────────────────────

# 生产部署：构建 + 复制到 /opt/chick + 启动（PostgreSQL + 端口 18080）
.PHONY: prod

# 默认 PostgreSQL DSN，可通过 CHICK_DB_DSN 覆盖
prod: build-prod ui-build
	@echo "=== 部署到 /opt/chick ==="
	sudo install -d /opt/chick/ui/dist
	sudo install -m 755 bin/chick-prod /opt/chick/chick-server
	sudo cp -r ui/dist/* /opt/chick/ui/dist/
	@echo "=== 停止旧进程 ==="
	-sudo pkill -f "/opt/chick/chick-server" 2>/dev/null || true
	@sleep 1
	@echo "=== 启动生产服务 ==="
	sudo CHICK_DB_DRIVER=postgres \
		CHICK_DB_DSN="$${CHICK_DB_DSN:-postgres://chick:chick@localhost:5432/chick?sslmode=disable}" \
		CHICK_PORT=18080 \
		CHICK_JWT_SECRET="$${CHICK_JWT_SECRET}" \
		CHICK_ALLOWED_ORIGINS="$${CHICK_ALLOWED_ORIGINS:-*}" \
		/bin/sh -c 'cd /opt/chick && nohup ./chick-server &>/tmp/chick-prod.log &'
	@sleep 2
	@echo "=== 健康检查 ==="
	@status=$$(curl -s -o /dev/null -w "%{http_code}" http://localhost:18080/health 2>/dev/null); \
	if [ "$$status" = "200" ]; then \
		echo "  ✅ 生产服务运行正常 (HTTP $$status)"; \
	else \
		echo "  ❌ 健康检查失败 (HTTP $$status)，查看日志: /tmp/chick-prod.log"; \
	fi
	@echo "=== 部署完成: http://0.0.0.0:18080 ==="

build-prod:
	go build -ldflags="-s -w" -o bin/chick-prod ./cmd/server/

# ─── 清理 ──────────────────────────────────────────────────

clean:
	rm -rf bin/ coverage.out
