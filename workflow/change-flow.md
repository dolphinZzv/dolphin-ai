# Change Flow

**原则：设计先行，代码随设计走。** 必须先完成设计变更并获评审通过，才能进入代码实现阶段。

每个变更需通过两级评审方可合并：

```mermaid
flowchart TD
    A(["1. Analysis"]) --> V1{"Scope defined?"}
    V1 -->|no| A
    V1 -->|yes| B(["2. Design"])
    B --> V2{"【L1 评审】<br/>Design approved?"}
    V2 -->|no| B
    V2 -->|yes| C(["3. Implementation"])
    C --> V3{"4. Self-Check<br/>go vet + go build"}
    V3 -->|no| C
    V3 -->|yes| V4{"Checklist verified?"}
    V4 -->|no| C
    V4 -->|yes| D(["5. Testing"])
    D --> V5{"go test -race ./...?"}
    V5 -->|no| C
    V5 -->|yes| E(["6. 【L2 评审】<br/>Code Review"])
    E --> V6{"Approved?"}
    V6 -->|no| C
    V6 -->|yes| F(["7. Merge & Release"])
    F --> V7{"CI passed?"}
    V7 -->|no| F
    V7 -->|yes| G["Done"]
```

## Two-Level Review

| Level | Gate | Scope | Verifier |
|-------|------|-------|----------|
| **L1** | Design → Implementation | 设计方案、架构影响、接口定义 | 架构师 / 资深开发者 |
| **L2** | Testing → Merge | 代码质量、安全性、测试覆盖 | 其他开发者 (≥ 1 人) |

L1 未通过不得进入实现阶段，L2 未通过不得合并。

## Verification Gates

| Gate | Verification | Verifier |
|------|-------------|----------|
| Analysis → Design | Scope defined | Self-check |
| Design → Implementation | **L1 评审**: 设计通过 | 架构师 |
| Implementation → Self-Check | `go vet ./...` + `go build ./...` | Self-check |
| Self-Check → Test | Checklist verified | Self-check |
| Test → Review | `go test -race ./...` | CI |
| Review → Merge | **L2 评审**: 代码通过 | 其他开发者 |
| Merge → Release | CI passes | CI |

## Prohibitions

- No direct commits to `main` or `develop`
- No skipping either level of review
- **No code changes before L1 design review is approved**
- No breaking changes without updating design docs
- No code commits without tests
