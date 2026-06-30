# 用 Agent Mesh 玩狼人杀

> 一句话开局：`python3 play_werewolf.py` —— 自动随机分配身份与模型，主持人委托各角色 agent 真实对局。

## 这是什么

把狼人杀的每个角色变成一个独立 AI agent（一个 dolphin 进程），主持人 agent 通过 Agent Mesh 的 `delegate_to_agent` 工具在夜晚委托各角色行动，每个角色用自己的 LLM 真实思考并回报。全程真实 API，本机多进程。

## 快速开始

```bash
cd delivery

# 编译 dolphin（含 agentmesh 接线）
cd .. && go build -o dolphin ./cmd/dolphin && cd delivery

# 真人参与（推荐）：你是 1 号玩家，其余 8 人由 AI 扮演，终端交互
python3 play_werewolf_human.py

# 全 AI 对局（旁观）：默认 9 人局，1 夜
python3 play_werewolf.py

# 12 人局，玩 3 夜
python3 play_werewolf.py --players 12 --rounds 3
```

脚本会：
1. 随机分配 9 个座位身份（狼人/预言家/女巫/守卫/猎人/村民）
2. 为每个角色 agent 随机选一个火山引擎模型
3. 启动 5 个角色进程 + 1 个主持人进程
4. 主持人按夜晚流程 `delegate_to_agent` 委托狼人/预言家/守卫/女巫
5. 收集各角色真实 LLM 回报，宣布结果

## 真人参与（推荐玩法）

`play_werewolf_human.py` 让你真正坐进牌桌：

```bash
python3 play_werewolf_human.py                    # 默认你是 1 号
python3 play_werewolf_human.py --name 张三 --seat 5  # 自定义名字和座位
```

- **你是 1 个真人玩家**，其余 8 人由主持人 agent 扮演
- 主持人随机给你发身份（只私下告诉你），夜晚若你是预言家/狼人/女巫/守卫，会暂停等你输入行动
- 白天你发言、投票，AI 玩家也发言投票，主持人综合裁决
- 通过终端交互：主持人用 `>>> 等待 1 号玩家行动:` 提示，你输入后回车提交

```
主持人: 🔮 1 号玩家（你），轮到你行动了！查验 2~9 号任意一名...
        >>> 等待 1 号玩家行动: 请输入要查验的玩家编号

你 > 3

主持人: 🔮 你查验了 3 号，结果是... 狼人！天亮了...
```

这是单人测试模式——1 真人 + 8 AI。后续可扩展到人机混合多座位（多个真人通过各自终端接入）。

## 角色与 Agent 映射

| 角色 | Agent | 能力标签 | 职责 |
|---|---|---|---|
| 主持人 | `moderator` | `orchestrate` | 调度夜晚行动、宣布结果 |
| 预言家 | `seer` | `divine` | 查验身份 |
| 女巫 | `witch` | `poison, save` | 解药/毒药 |
| 守卫 | `guard` | `protect` | 守护 |
| 猎人 | `hunter` | `shoot` | 死后开枪 |
| 狼人 | `werewolf` | `kill` | 夜杀 |

主持人是 client（orchestrator），其余角色是 server（被委托方）。每个角色一个目录（`runtime_play/<role>/`），含独立 config.yaml + memory + sessions。

## 可用模型（火山引擎）

```yaml
volcengine_agent:
  provider: volcengine
  api_type: openai
  api_key: "ark-..."
  base_url: "https://ark.cn-beijing.volces.com/api/plan/v3"
```

已注册 provider 的模型（`internal/llm/models/` 下有文件）：
- `deepseek-v4-pro`
- `deepseek-v4-flash`
- `minimax-m3`
- `glm-5.2`

> `kimi-k2.7-code` / `doubao-seed-2.0-code` 暂无 provider 文件，不可用。每个角色随机选一个可用模型。

## 委托是怎么工作的

```
你: "天黑了，各角色行动"
  ↓ A2A tasks/send
moderator LLM 收到 → 决定调用 delegate_to_agent(agent="seer", task="查验 3 号")
  ↓ AgentMesh.Delegate
  ↓ Router → seer (127.0.0.1:8201)
  ↓ A2AClient.SendTask → tasks/send
seer LLM 收到 "查验 3 号" → 真实调用火山 API → 返回 "3 号是狼人"
  ↓ DelegateResult.Content
moderator LLM 收到工具结果 → 继续委托 guard、werewolf...
  ↓ 汇总
moderator 宣布夜晚结果
```

每个委托带 OpenTelemetry trace，跨 agent 串联。日志里能看到：
```
agent.delegate.sent   to="seer"   depth=1
agent.delegate.received from="seer" status="completed"
tool.complete tool="delegate_to_agent" output="3 号是狼人"
```

## 手动玩（不写脚本）

### 1. 各角色 config（每个角色一个目录）

`seer/config.yaml`:
```yaml
agent:
  name: seer
  workspace: ./runtime/seer
a2a:
  enabled: true
  addr: "127.0.0.1:8201"
agents:
  enabled: true
  name: seer
  listen_addr: "127.0.0.1:8201"
  capabilities: ["divine"]
  max_delegation_depth: 1
llm:
  use: volcengine_agent/glm-5.2
  volcengine_agent:
    provider: volcengine
    api_type: openai
    api_key: "ark-..."
    base_url: "https://ark.cn-beijing.volces.com/api/plan/v3"
    models:
      - name: "glm-5.2"
memory:
  dir: ./runtime/seer/memory
session:
  dir: ./runtime/seer/sessions
tui:
  enabled: false
dream:
  enabled: false
```

### 2. 主持人 config（注册角色为 remote peer）

```yaml
agents:
  enabled: true
  name: moderator
  listen_addr: "127.0.0.1:8200"
  task_timeout: "120s"
  max_delegation_depth: 3
  remote:
    - name: seer
      addr: "127.0.0.1:8201"
      capabilities: ["divine"]
    # ... 其余角色
```

### 3. 启动 + 委托

```bash
# 起角色
dolphin --config seer/config.yaml &
dolphin --config witch/config.yaml &
# ...

# 起主持人
dolphin --config moderator/config.yaml

# 通过 A2A 发委托
curl -X POST http://127.0.0.1:8200/jsonrpc -H "Content-Type: application/json" -d '{
  "jsonrpc":"2.0","id":"1","method":"tasks/send",
  "params":{"message":{"role":"user","parts":[{"text":"天黑了，让预言家查 3 号"}]}}
}'
```

## E2E 测试

`e2e_werewolf.py` 是不带交互的纯验证脚本，断言主持人成功委托 seer/guard/werewolf：

```bash
python3 e2e_werewolf.py
```

它会启动 6 个进程，让主持人委托 3 个角色，校验链路打通。已验证通过：4 个角色用 4 个不同模型，全部真实 API 调用成功。

## 远程多机（后续）

当前是本机多进程。后期接入远程机器时：
- 角色进程跑在远程机器，`a2a.addr` 绑 `0.0.0.0:port`
- 主持人 `agents.remote` 里填远程 IP
- 启用 `agents.tls.enabled: true` + mTLS 证书
- 或启用 `agents.discovery.mode: gossip` 自动发现局域网 agent

设计文档见 [`design/modules/agent-mesh.md`](../../design/modules/agent-mesh.md)。

## 故障排查

| 现象 | 原因 | 解决 |
|---|---|---|
| `llm: unknown model` | 模型无 provider 文件 | 用 4 个可用模型之一 |
| `message has no text content` | client/server 协议不匹配 | 已修复，确保用最新 dolphin 二进制 |
| `A2A 传输不支持交互式权限请求` | 工具被权限拦 | `delegate_to_agent` 已免权限；其它工具在 permissions.json 加 allow |
| 角色启动失败 | 端口占用 / config 错 | 看 `runtime/<role>/stdout.log` |
