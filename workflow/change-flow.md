# Change Flow

每个变更依次通过以下关卡：

```mermaid
flowchart TD
    A(["1. Analysis"]) --> V1{"Scope defined?"}
    V1 -->|no| A
    V1 -->|yes| B(["2. Design"])
    B --> V2{"Review passed?"}
    V2 -->|no| B
    V2 -->|yes| C(["3. Implementation"])
    C --> V3{"4. Self-Check<br/>go vet + go build"}
    V3 -->|no| C
    V3 -->|yes| V4{"Checklist verified?"}
    V4 -->|no| C
    V4 -->|yes| D(["5. Testing"])
    D --> V5{"go test -race ./...?"}
    V5 -->|no| C
    V5 -->|yes| E(["6. Code Review"])
    E --> V6{"Other Agent approved?"}
    V6 -->|no| C
    V6 -->|yes| F(["7. Merge & Release"])
    F --> V7{"CI passed?"}
    V7 -->|no| F
    V7 -->|yes| G["Done"]
```

## Verification Gates

| Gate | Verification | Verifier |
|------|-------------|----------|
| Analysis → Design | Scope defined | Self-check |
| Design → Implementation | Design proposal reviewed | Reviewer |
| Implementation → Self-Check | `go vet ./...` + `go build ./...` | Self-check |
| Self-Check → Test | Checklist verified | Self-check |
| Test → Review | `go test -race ./...` | CI |
| Review → Merge | At least 1 approval | Other Agent |
| Merge → Release | CI passes | CI |

## Prohibitions

- No direct commits to `main` or `develop`
- No skipping code review
- No breaking changes without updating design docs
- No code commits without tests
