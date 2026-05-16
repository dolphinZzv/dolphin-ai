# Context File Variable Injection

## Overview

Context files (PREFACE.md, BUILTIN_SKILLS.md, AGENTS.md, RULES.md, USER.md, SYSTEM.md) use Go's `text/template` for variable injection. Templates have access to flattened config values, environment variables, and a `default` helper.

## Why `text/template`

- Standard library — zero new dependencies
- `{{or .val "fallback"}}` — clean single-line fallback
- Custom functions — `default`, `env` for explicit intent
- Conditionals, ranges — full power when needed, not forced
- Safe — templates execute in-process, no shell injection

## Syntax

```go
// Data context passed to template:
type renderData struct {
    Config map[string]string  // dotted paths like "llm.model", "session.max_loop"
    Env    map[string]string  // os.Environ() as map at startup
}
```

Template access:

```
{{.Config.name}}                        — config value
{{.Config.llm.model}}                   — nested via dotted key in flat map
{{or .Config.name "dolphin"}}           — fallback: use "dolphin" if name is empty
{{default .Config.model "gpt-4o"}}      — explicit default function
{{env "HOME"}}                           — environment variable
{{or (env "LANG") "en_US.UTF-8"}}        — env with fallback
{{if .Config.id}}ID: {{.Config.id}}{{end}} — conditional
```

### Custom Functions

| Function | Example | Behavior |
|----------|---------|----------|
| `default` | `{{default .Config.name "dolphin"}}` | Returns fallback if first arg is empty string |
| `env` | `{{env "HOME"}}` | Returns `os.Getenv(key)` |

`default` and `or` differ: `or` returns the first non-empty arg, `default` explicitly means "use this when not set".

## Data Context

Config is flattened into a `map[string]string` with dotted paths:

```go
data := renderData{
    Config: map[string]string{
        "name":             "dolphin",
        "id":               "crqgvlh8tio82qgl5u7g",
        "llm.model":        "gpt-4o",
        "llm.max_tokens":   "4096",
        "session.max_loop": "50",
    },
    Env: envMap,
}
```

All keys from `setDefaults()` are present — built-in keys are **never empty** in the data map.

## Fallback

Two mechanisms, no special syntax needed:

```
{{.Config.name}}                           → "dolphin" (always has default)
{{or .Config.custom_field "not set"}}      → "not set" (empty → fallback)
{{default .Config.model "gpt-4o"}}         → "gpt-4o" (explicit default)
{{or (env "EDITOR") "vi"}}                → "vi" (env missing → fallback)
```

## Template Execution

Templates are parsed and executed at **load time** (first read, cached by mtime):

```
loadFile(path)
  → os.ReadFile(path)
  → if renderData != nil:
      tmpl, err := template.New(path).Funcs(funcMap).Parse(content)
      if err != nil → zap.Warn, return raw content
      tmpl.Execute(buf, renderData)
      return buf.String()
  → return content
```

Parse errors never break the agent — raw content is the fallback.

## Implementation

### Builder Changes

```go
type Builder struct {
    projectDir string
    userDir    string
    systemDir  string
    statCache  map[string]cachedFile
    renderData *renderData
}

func (b *Builder) SetRenderData(cfg *config.Config) {
    envMap := make(map[string]string)
    for _, kv := range os.Environ() {
        if k, v, ok := strings.Cut(kv, "="); ok {
            envMap[k] = v
        }
    }
    b.renderData = &renderData{
        Config: flattenConfig(cfg),
        Env:    envMap,
    }
}
```

### flattenConfig

Walks the Config struct via viper to produce a flat dotted map:

```go
func flattenConfig(v *viper.Viper) map[string]string {
    out := make(map[string]string)
    for _, k := range v.AllKeys() {
        out[k] = v.GetString(k)
    }
    return out
}
```

### Wire-up (in Agent.Run)

```go
a.ctxBuilder.SetRenderData(a.cfg)
systemPrompt, err := a.ctxBuilder.Build()
```

## Example: AGENTS.md

User writes:

```markdown
# Agent Identity

You are {{default .Config.name "dolphin"}}, instance {{.Config.id}}.
Model: {{default .Config.llm.model "unknown"}}
Shell: {{or (env "SHELL") "/bin/sh"}}

{{if .Env.LANG}}
System language: {{.Env.LANG}}
{{end}}

## Commands
/help, /status, /exit, /mcp, /provider
```

Expanded:

```
# Agent Identity

You are dolphin, instance crqgvlh8tio82qgl5u7g.
Model: deepseek-v4-flash.
Shell: /bin/zsh

System language: zh_CN.UTF-8

## Commands
/help, /status, /exit, /mcp, /provider
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| `text/template` over Mustache | Stdlib, `{{or}}` fallback, no new dependency |
| Config as flat `map[string]string` | No reflection at render time |
| `default` function | Distinguishes "unset" fallback from `or`'s "first truthy" |
| `env` as function not map field | Explicit namespace, avoids `.Env` collision paranoia |
| Template errors → raw content | Context files must never break the agent |
| All default keys pre-populated | `{{.Config.name}}` never empty, no surprises |
