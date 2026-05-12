# 质量保障与工程规范

## 1. 强制工具链

以下工具加入 CI，提交前必须通过：

| 工具 | 用途 | 捕获的问题 |
|------|------|-----------|
| `go vet ./...` | Go 静态分析 | 可疑构造、死代码 |
| `golangci-lint` (errcheck, staticcheck) | 错误检查 + 未使用代码 | `_ = err`, 定义未调用的函数 |
| `go test ./... -count=1 -cover` | 测试 + 覆盖率 | 功能回归 |
| `npx tsc --noEmit` | TypeScript 类型检查 | 类型错误 |
| `npx tailwindcss --check` | Tailwind 类名校验 | `border-l-3` 等无效类名 |
| `npx eslint src/` | React/TS lint | 渲染中 navigate 等 anti-pattern |

**推荐 `make check` 一键运行全部。**

## 2. 常见问题预防清单

### GraphQL Resolver

- **Helper 函数放独立文件** — `schema.resolvers.go` 由 gqlgen 自动生成/覆盖，所有自定义辅助函数（`requireAuth`、`requireProjectOwner` 等）必须放在单独文件（如 `authz.go`）
- **权限检查矩阵** — 每个 mutation 必须明确标注所需的权限级别：

  | Mutation | 权限 |
  |----------|------|
  | createProject | requireAuth |
  | updateProject | requireProjectOwner |
  | deleteProject | requireProjectOwner |
  | add/update/removeProjectMember | requireProjectOwner |
  | create/update/deleteIssue | requireIssueProjectMember |
  | transitionIssue | requireIssueProjectMember |
  | add/removeAssignee | requireIssueProjectMember |
  | addComment | requireIssueProjectMember |
  | update/deleteComment | 作者本人 (authorID == agentID) |
  | createLabel | requireProjectOwner |
  | deleteLabel | requireProjectOwner |
  | createMilestone | requireProjectOwner |
  | deleteMilestone | requireProjectOwner |

- **Subscription 生命周期** — 确保 `cancel()` 在 `ctx.Done()` 时被调用，声明模式：
  ```go
  cancel := func() {}
  if r.EventBus != nil {
      cancel = r.EventBus.Subscribe(...)
  }
  go func() {
      <-ctx.Done()
      cancel()
      close(ch)
  }()
  ```

### 错误处理

- `_ =` 禁止出现在 PR 中 — 要么处理错误（return / log），要么确认确实可忽略并在注释中说明理由
- `// nolint: errcheck` 必须有注释解释为什么忽略
- 所有 `json.Unmarshal`、`json.Decode` 的返回值必须检查

### 前端

- **API 调用统一封装** — 通过 `src/lib/graphql.ts` 的 `gql()` 函数，禁止页面直接 `fetch("/graphql", ...)` + 手动拼 `Authorization` header
- **渲染中不可调用 navigate** — 使用 `<Navigate>` 组件替代
- **Tailwind 类名复查** — 看板等用了 `border-l-3` 等不存在类名的，提交前 `npx tailwindcss --check`

### 死代码检测

- 定义但从未调用的导出/非导出函数会被 `staticcheck U1000` 捕获。示例如 `WatchOfflineTimeout` 只在 `engine.go` 定义但从未启动的 goroutine。
- `go.mod` 中未使用的依赖会被 `go mod tidy` 清理。

## 3. 代码审查清单

每次 PR 审查时逐项检查：

- [ ] 新增 mutation/query 是否有对应的权限检查？
- [ ] Subscription 是否有 cancel 机制？
- [ ] 所有错误是否被处理（不是 `_ =`）？
- [ ] 前端是否使用 `gql()` 而不是直接 fetch？
- [ ] 是否有死代码（定义未调用的函数/变量）？
- [ ] 是否有 Tailwind 无效类名？
- [ ] gqlgen 重新生成后，自定义 helper 是否还在（没被覆盖）？
- [ ] 是否有级联删除遗漏（删除 Project 时是否清理了子资源）？
