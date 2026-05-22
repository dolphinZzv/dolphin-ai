# Credentials MCP Tools

## Overview

添加 credentials 搜索和获取 MCP 工具，允许 LLM 通过 MCP 获取存储的凭证，用于需要认证的网站登录授权场景。

## Credential Acquisition Flow

凭证可以通过以下方式添加到系统中：

### 1. 手动创建 JSON 文件

用户直接编辑 `.dolphin/credentials.json`：

```json
{
  "credentials": [
    {
      "name": "github-api",
      "type": "api_key",
      "url": "https://api.github.com",
      "username": "",
      "secret": "ghp_xxxx",
      "comment": "GitHub API token for CI"
    }
  ]
}
```

### 2. LLM 引导添加（推荐）

当 LLM 发现缺少凭证时，可以：

1. 调用 `credentials_add` 工具申请添加凭证
2. 系统提示用户输入凭证信息
3. 用户确认后，凭证保存到文件

```
User: "帮我登录 GitHub"
LLM: "我需要 GitHub API token。请提供以下信息：
      - Token (ghp_xxx 格式)
      - 或者告诉我去 config 设置"
User: "token is ghp_xxxx"
LLM: -> credentials_add tool
     -> 保存凭证
     -> 继续执行任务
```

### 3. credentials_add 工具

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "凭证名称 (唯一标识，如 'github-api')"
    },
    "type": {
      "type": "string",
      "enum": ["api_key", "oauth", "aws_access_key", "password", "ssh_key", "client_secret"],
      "description": "凭证类型"
    },
    "url": {
      "type": "string",
      "description": "API 端点 URL (可选)"
    },
    "username": {
      "type": "string",
      "description": "用户名 (可选)"
    },
    "secret": {
      "type": "string",
      "description": "密钥/API Key/密码"
    },
    "comment": {
      "type": "string",
      "description": "备注说明 (可选)"
    }
  },
  "required": ["name", "type", "secret"]
}
```

**Output:**
```json
{
  "success": true,
  "name": "github-api",
  "message": "Credential saved successfully"
}
```

### 4. credentials_delete 工具

删除不再需要的凭证：

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "凭证名称"
    }
  },
  "required": ["name"]
}
```

## Use Cases

- 用户登录需要 API key 的服务
- OAuth token 获取
- SSH key / password retrieval
- 网站 Basic Auth 凭证

## Configuration Design

### Credentials Config

```yaml
credentials:
  enabled: true
  store: file              # "file" (json file) or "keychain" (future)
  path: .dolphin/credentials.json
  # 搜索时返回的字段白名单（不返回 secret）
  safe_fields:
    - name
    - type
    - url
    - username
    - comment
  # 允许 LLM 获取的凭证模式（glob 匹配），空=允许所有
  allow_only:
    - "github-*"
    - "aws-*"
    - "openai-*"
    - "anthropic-*"
```

### Credentials Store Structure

```json
{
  "credentials": [
    {
      "name": "github-api",
      "type": "api_key",
      "url": "https://api.github.com",
      "username": "",
      "secret": "ghp_xxxx",
      "comment": "GitHub API token for CI"
    },
    {
      "name": "aws-prod",
      "type": "aws_access_key",
      "url": "",
      "username": "AKIAIOSFODNN7EXAMPLE",
      "secret": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      "comment": "AWS Production access key"
    },
    {
      "name": "gmail-oauth",
      "type": "oauth",
      "url": "https://gmail.googleapis.com",
      "username": "user@gmail.com",
      "secret": "ya29.xxx",
      "comment": "Gmail OAuth token"
    }
  ]
}
```

## MCP Tools Design

### 1. credentials_search

搜索凭证列表，支持过滤。

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "搜索关键词（匹配 name, url, username, comment）"
    },
    "type": {
      "type": "string",
      "enum": ["api_key", "oauth", "aws_access_key", "password", "ssh_key", ""],
      "description": "按类型过滤"
    },
    "limit": {
      "type": "integer",
      "description": "返回数量限制，默认 10"
    }
  }
}
```

**Output:**
```json
{
  "credentials": [
    {
      "name": "github-api",
      "type": "api_key",
      "url": "https://api.github.com",
      "username": "",
      "comment": "GitHub API token for CI"
    }
  ],
  "total": 1
}
```

### 2. credentials_get

获取单个凭证的完整信息（包含 secret）。

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "凭证名称"
    }
  },
  "required": ["name"]
}
```

**Output:**
```json
{
  "name": "github-api",
  "type": "api_key",
  "url": "https://api.github.com",
  "username": "",
  "secret": "ghp_xxxx",
  "comment": "GitHub API token for CI"
}
```

**Error:**
```json
{
  "error": "credential not found: github-api"
}
```

## Security Design

### 1. 权限控制

- MCP tools 需要在 config 中启用
- 可以配置 allowlist 指定 LLM 可以获取哪些凭证

```yaml
credentials:
  enabled: true
  allow_only:
    - "github-*"
    - "aws-*"
    - "openai-*"
```

### 2. 日志记录

所有 credentials 访问都需要记录日志：

```go
zap.Info("credentials accessed",
    zap.String("tool", "credentials_get"),
    zap.String("name", name),
    zap.String("client", clientInfo),
)
```

### 3. Secret 脱敏

日志中和 API 返回的 secret 应该部分脱敏：
- `ghp_xxxx...yyyy` (保留前4后4)
- `AKIA...IEXX` (保留前4后4)

## Implementation Design

### 文件结构

```
internal/mcp/credentials/
  credentials.go    # MCP Tool 实现
  store.go         # 凭证存储管理
  types.go         # 类型定义
```

### 类型定义

```go
type Credential struct {
    Name     string `json:"name"`
    Type     string `json:"type"`
    URL      string `json:"url,omitempty"`
    Username string `json:"username,omitempty"`
    Secret   string `json:"secret,omitempty"`
    Comment  string `json:"comment,omitempty"`
}

type CredentialsConfig struct {
    Enabled    bool     `mapstructure:"enabled"`
    Store      string   `mapstructure:"store"`
    Path       string   `mapstructure:"path"`
    SafeFields []string `mapstructure:"safe_fields"`
    AllowOnly  []string `mapstructure:"allow_only"`
}

// Validate 检查配置合法性
func (c *CredentialsConfig) Validate() error {
    if !c.Enabled {
        return nil
    }
    if c.Path == "" {
        return errors.New("credentials path is required")
    }
    if c.Store != "file" {
        return errors.New("only 'file' store is supported currently")
    }
    return nil
}
```

### 脱敏函数

```go
func MaskSecret(secret string) string {
    if len(secret) <= 8 {
        return "***"
    }
    return secret[:4] + "..." + secret[len(secret)-4:]
}
```

### AllowOnly 检查

```go
func isAllowed(name string, patterns []string) bool {
    if len(patterns) == 0 {
        return true
    }
    for _, pattern := range patterns {
        if matched, _ := filepath.Match(pattern, name); matched {
            return true
        }
    }
    return false
}
```

### 日志记录

```go
func logAccess(tool, name, maskedSecret string) {
    zap.Info("credentials accessed",
        zap.String("tool", tool),
        zap.String("name", name),
        zap.String("masked_secret", maskedSecret),
        zap.String("timestamp", time.Now().Format(time.RFC3339)),
    )
}
```

### Store 接口

```go
type Store interface {
    Search(query string, credType string, limit int) ([]Credential, error)
    Get(name string) (*Credential, error)
    List() ([]Credential, error)
    Add(cred *Credential) error
    Delete(name string) error
    Close() error
}
```

### 文件存储实现

```go
type FileStore struct {
    path   string
    mu     sync.RWMutex
    data   *CredentialFile
}

type CredentialFile struct {
    Credentials []Credential `json:"credentials"`
}
```

## Error Types

```go
var (
    ErrCredentialNotFound = errors.New("credential not found")
    ErrCredentialDisabled  = errors.New("credentials disabled")
    ErrAccessDenied       = errors.New("access denied: credential not in allowlist")
)

type CredentialError struct {
    Name  string
    Type  string // "not_found", "disabled", "denied"
    Msg   string
}

func (e *CredentialError) Error() string {
    return fmt.Sprintf("credential error: %s (%s)", e.Name, e.Type)
}
```

## Credential Types

凭证类型枚举（支持扩展）：

| Type | Description | Example |
|------|-------------|---------|
| `api_key` | API Key | GitHub API token |
| `oauth` | OAuth Token | Gmail OAuth |
| `aws_access_key` | AWS Access Key | AWS IAM |
| `password` | Password | HTTP Basic Auth |
| `ssh_key` | SSH Private Key | Git SSH |
| `client_secret` | Client Secret | OAuth Client |

## Store Interface (Future Extensible)

```go
type Store interface {
    Search(query string, credType string, limit int) ([]Credential, error)
    Get(name string) (*Credential, error)
    List() ([]Credential, error)
    Close() error
}

type StoreType string

const (
    StoreTypeFile     StoreType = "file"
    StoreTypeKeychain StoreType = "keychain"  // future
    StoreTypeVault     StoreType = "vault"    // future
)

func NewStore(cfg *CredentialsConfig) (Store, error) {
    switch StoreType(cfg.Store) {
    case StoreTypeFile:
        return newFileStore(cfg)
    case StoreTypeKeychain:
        return newKeychainStore(cfg)  // TODO: implement
    case StoreTypeVault:
        return newVaultStore(cfg)      // TODO: implement
    default:
        return nil, fmt.Errorf("unsupported store type: %s", cfg.Store)
    }
}
```

## Security Checklist

- [x] `enabled` flag to enable/disable
- [x] `allow_only` glob patterns for credential access control
- [x] `safe_fields` white-list for search results (no secrets)
- [x] Secret masking in logs
- [x] Access logging with timestamp
- [x] Input validation for name/query
- [x] Validate() configuration check

## 状态

- 创建时间: 2026-05-22
- 状态: 设计完成，待实现

<!-- last-modified: 2026-05-22 -->