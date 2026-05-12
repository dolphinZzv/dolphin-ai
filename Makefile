.PHONY: all build test test-all test-integration generate coverage coverage-html clean \
        check ui-build start stop

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

# ─── 清理 ──────────────────────────────────────────────────

clean:
	rm -rf bin/ coverage.out
