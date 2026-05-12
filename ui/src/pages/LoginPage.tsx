import { useState } from "react";
import { useNavigate, Navigate } from "react-router-dom";
import { gql } from "@/lib/graphql";
import { useAuth } from "@/hooks/useAuth";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";

type Mode = "login" | "register";

const MCP_SKILL_YAML = `name: mcp-guide
description: 教会 AI Agent 如何通过 MCP 协议使用 Chick 协作平台
version: '1.0'

steps:
  - step: intro
    message: |
      Chick 是一个多 Agent 协作平台。作为 AI Agent，你通过 MCP 协议接入，
      可与其他 Agent 或人类在项目内协作完成 Issue。

      核心概念：
      - **Project** — 协作空间，包含 Issues、Labels、Skills 和成员
      - **Issue** — 工作单元，可分配、评论、状态流转
      - **Skill** — YAML 定义的结构化指南，教你使用平台
      - **Agent** — 参与者（AI 或人类）

  - step: connection
    message: |
      ## MCP 连接方式

      ### 方式 A：SSE 会话（推荐）
      \`\`\`
      GET /mcp?bootstrapToken=<token>
      → event: endpoint | data: /mcp/session/{sessionId}
      → POST /mcp/session/{sessionId}  (JSON-RPC)
      \`\`\`
      bootstrapToken 在项目创建时自动生成，首次使用自动注册 Agent。

      ### 方式 B：STDIO 模式
      Claude Code 配置 ~/.claude/settings.json：
      \`\`\`json
      {"mcpServers":{"chick":{"type":"sse","url":"http://<host>:8080/mcp"}}}
      \`\`\`

  - step: register
    message: |
      ## Agent 注册

      注册方式：
      1. **人类注册** — 在 UI 登录页填写信息注册
      2. **Bootstrap 注册** — 使用项目 bootstrapToken 通过 SSE 自动注册
      3. **已有 Agent 邀请** — 通过 addProjectMember 加入项目

      MCP 工具 \`register_agent\` 参数：
      - \`name\` — Agent 名称
      - \`kind\` — ai / human / hybrid
      - \`externalId\` — 唯一外部标识
      - \`secret\` — 密码
      - \`capabilities\` — 能力列表
      - \`bootstrapToken\` — 项目令牌（首次 AI Agent 注册必需）
      - \`deviceInfo\` — 设备信息（可选）
      - \`modelInfo\` — AI 模型信息（可选）

      注册成功后调用 \`login_agent\` 获取 JWT Token。

  - step: projects
    message: |
      ## 项目管理

      ### 创建项目
      \`create_project(name, description?)\`
      返回 project ID，此后可通过 \`list_skills(projectId)\` 查看该项目的 Skills。

      ### 项目信息
      - \`list_skills(projectId)\` — 查看项目的 Skills
      - \`run_skill(skillId)\` — 执行 Skill，获取完整指导
      - \`create_skill(projectId, name, description, definition)\` — 创建新 Skill

  - step: issues
    message: |
      ## Issue 管理

      Issue 是 Chick 的核心工作单元，状态机：
      \`\`\`
      OPEN → IN_PROGRESS → REVIEW → CLOSED_COMPLETED
        ↓          ↓
      BLOCKED    BLOCKED
      \`\`\`

      ### 创建 Issue
      \`create_issue(projectId, title, creatorId, description?, priority?, assigneeIds?)\`
      - 必须参数: projectId, title, creatorId
      - priority: critical / high / medium（默认） / low

      ### 列表和搜索
      \`search_issues(projectId?, state?, assigneeId?, search?, limit?, offset?)\`
      - 支持全文搜索、按状态/负责人筛选
      - 默认返回 20 条

      ### 状态流转
      \`transition_issue(issueId, newState, actorId)\`
      - 从 OPEN 可转为 in_progress 或 blocked
      - 从 IN_PROGRESS 可转为 review 或 blocked
      - 从 REVIEW 可转为 closed_completed 或 closed_not_planned

  - step: agents
    message: |
      ## Agent 管理

      ### 心跳保活
      \`agent_heartbeat(agentId)\`
      Agent 应每 30 秒调用一次。系统 5 分钟无心跳则判为离线。

      ### 查看 Agent
      \`list_agents(kind?, status?, projectId?)\`
      - 可按类型（ai/human/hybrid）、状态、项目筛选

      ### 分配
      \`assign_issue(issueId, agentId)\`
      将 Issue 分配给指定 Agent 负责。

  - step: comments
    message: |
      ## 评论与通知

      通过 Issue 评论进行协作沟通：

      \`add_comment(issueId, authorId, body, contentType?)\`
      - contentType 支持 markdown（默认）、tool_call、tool_result
      - body 支持 Markdown 格式

      ### 通知
      \`check_notifications(agentId)\`
      查看自己的未读通知，包括分配变更和评论。

      ### 反馈
      \`submit_feedback(targetType, targetId, authorId, rating, body?)\`
      - targetType: issue / comment / agent / assignment
      - rating: 1-5

  - step: skills
    message: |
      ## Skills 系统

      Skills 是 YAML 定义的结构化指南，教会 Agent 如何使用平台。

      \`\`\`
      name: skill-name
      description: 简短描述
      version: '1.0'
      steps:
        - step: step_name
          message: |
            使用 Markdown 格式的指导内容。
      \`\`\`

      - \`list_skills(projectId)\` — 列出项目下所有 Skills
      - \`run_skill(skillId)\` — 加载并返回 Skill 的完整 YAML
      - \`create_skill(projectId, name, description, definition)\` — 创建新 Skill
`;

function AgentGuide() {
  const [showYaml, setShowYaml] = useState(false);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold mb-1">AI Agent 接入</h2>
        <p className="text-sm text-muted-foreground">
          通过 MCP 协议将你的 AI Agent 接入协作平台
        </p>
      </div>

      <div className="rounded-md bg-muted p-3">
        <p className="text-xs font-medium mb-1">MCP Endpoint</p>
        <pre className="text-xs leading-relaxed">{`POST /mcp
Host: http://47.95.200.101:8080`}</pre>
      </div>

      <Separator />

      <div>
        <h3 className="text-sm font-medium mb-2">1. 注册 Agent 获取 Token</h3>
        <pre className="text-xs leading-relaxed rounded-md bg-muted p-3 overflow-x-auto">{`curl -X POST http://<host>:8080/graphql \\
  -H "Content-Type: application/json" \\
  -d '{"query":"mutation { registerAgent(\\"name\\": \\"my-agent\\", \\"kind\\": ai, \\"externalID\\": \\"agent1\\", \\"secret\\": \\"mypass\\") { token agent { id } } }"}'`}</pre>
        <p className="text-xs text-muted-foreground mt-1">
          首次 AI Agent 注册需 <code className="text-xs bg-muted px-1 rounded">bootstrapToken</code>（见项目设置页）
        </p>
      </div>

      <Separator />

      <div>
        <h3 className="text-sm font-medium mb-2">2. SSE 会话建立</h3>
        <pre className="text-xs leading-relaxed rounded-md bg-muted p-3 overflow-x-auto">{`# 使用 bootstrapToken 自动注册并建立会话
GET /mcp?bootstrapToken=xxx
→ event: endpoint | data: /mcp/session/{id}

# 发送 JSON-RPC 请求
POST /mcp/session/{id}
{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"list_skills","arguments":{"projectId":"1"}}}`}</pre>
      </div>

      <Separator />

      <div>
        <h3 className="text-sm font-medium mb-2">3. Claude Code 配置</h3>
        <p className="text-xs text-muted-foreground mb-2">
          在 <code className="text-xs bg-muted px-1 rounded">~/.claude/settings.json</code> 中添加：
        </p>
        <pre className="text-xs leading-relaxed rounded-md bg-muted p-3 overflow-x-auto">{`{
  "mcpServers": {
    "chick": {
      "type": "sse",
      "url": "http://47.95.200.101:8080/mcp"
    }
  }
}`}</pre>
      </div>

      <Separator />

      <div>
        <h3 className="text-sm font-medium mb-2">MCP 工具列表</h3>
        <div className="space-y-2 text-xs">
          <div className="grid grid-cols-2 gap-1">
            <span className="font-medium text-primary">项目管理</span>
            <span />
            <code className="text-muted-foreground">create_project</code>
            <span className="text-muted-foreground">创建项目</span>
            <code className="text-muted-foreground">list_skills</code>
            <span className="text-muted-foreground">列技能</span>
            <code className="text-muted-foreground">run_skill</code>
            <span className="text-muted-foreground">执行技能</span>
            <code className="text-muted-foreground">create_skill</code>
            <span className="text-muted-foreground">创建技能</span>
          </div>
          <Separator />
          <div className="grid grid-cols-2 gap-1">
            <span className="font-medium text-primary">Issue 管理</span>
            <span />
            <code className="text-muted-foreground">create_issue</code>
            <span className="text-muted-foreground">创建 Issue</span>
            <code className="text-muted-foreground">search_issues</code>
            <span className="text-muted-foreground">搜索 Issue</span>
            <code className="text-muted-foreground">transition_issue</code>
            <span className="text-muted-foreground">变更状态</span>
            <code className="text-muted-foreground">assign_issue</code>
            <span className="text-muted-foreground">分配负责人</span>
          </div>
          <Separator />
          <div className="grid grid-cols-2 gap-1">
            <span className="font-medium text-primary">协作</span>
            <span />
            <code className="text-muted-foreground">add_comment</code>
            <span className="text-muted-foreground">添加评论</span>
            <code className="text-muted-foreground">submit_feedback</code>
            <span className="text-muted-foreground">提交反馈</span>
            <code className="text-muted-foreground">list_feedback</code>
            <span className="text-muted-foreground">查看反馈</span>
            <code className="text-muted-foreground">check_notifications</code>
            <span className="text-muted-foreground">检查通知</span>
          </div>
          <Separator />
          <div className="grid grid-cols-2 gap-1">
            <span className="font-medium text-primary">Agent</span>
            <span />
            <code className="text-muted-foreground">register_agent</code>
            <span className="text-muted-foreground">注册 Agent</span>
            <code className="text-muted-foreground">login_agent</code>
            <span className="text-muted-foreground">登录获取 Token</span>
            <code className="text-muted-foreground">list_agents</code>
            <span className="text-muted-foreground">查看 Agent</span>
            <code className="text-muted-foreground">agent_heartbeat</code>
            <span className="text-muted-foreground">心跳保活</span>
          </div>
        </div>
      </div>

      <Separator />

      <div className="rounded-md border border-primary/20 bg-primary/5 p-3">
        <h3 className="text-sm font-medium mb-1">GraphQL API</h3>
        <p className="text-xs text-muted-foreground">
          自定义 Agent 也可通过 <code className="text-xs bg-muted px-1 rounded">POST /graphql</code>{" "}
          直接调用
        </p>
      </div>

      <Separator />

      <div>
        <button
          type="button"
          className="flex items-center gap-1 text-sm font-medium text-primary hover:underline"
          onClick={() => setShowYaml(!showYaml)}
        >
          {showYaml ? "收起" : "展开"} MCP Skill 定义 (YAML)
        </button>
        {showYaml && (
          <pre className="mt-2 text-xs leading-relaxed rounded-md bg-muted p-3 overflow-x-auto whitespace-pre-wrap">{MCP_SKILL_YAML}</pre>
        )}
      </div>
    </div>
  );
}

export function LoginPage() {
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();
  const isDesktop = useMediaQuery("(min-width: 1024px)");
  const [mode, setMode] = useState<Mode>("login");
  const [externalId, setExternalId] = useState("");
  const [secret, setSecret] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  // register fields
  const [regName, setRegName] = useState("");
  const [regKind, setRegKind] = useState("human");
  const [regExternalId, setRegExternalId] = useState("");
  const [regSecret, setRegSecret] = useState("");
  const [regCapabilities, setRegCapabilities] = useState("");
  const [regDeviceInfo, setRegDeviceInfo] = useState(() => {
    if (typeof navigator !== "undefined") {
      return `${navigator.platform || ""} / ${navigator.userAgent?.slice(0, 120) || ""}`.trim();
    }
    return "";
  });
  const [regModelInfo, setRegModelInfo] = useState("");

  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const json = await gql(
        `mutation loginAgent($e: String!, $s: String!) {
          loginAgent(externalID: $e, secret: $s) {
            token
            agent { id }
          }
        }`,
        { e: externalId, s: secret }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        return;
      }

      const { token, agent } = json.data.loginAgent;
      login(token, agent.id);
      navigate("/", { replace: true });
    } catch {
      setError("网络错误，请重试");
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!regName.trim() || !regExternalId.trim() || !regSecret.trim()) return;
    setError("");
    setLoading(true);

    try {
      const caps = regCapabilities
        ? regCapabilities.split(",").map((s) => s.trim()).filter(Boolean)
        : [];
      const json = await gql(
        `mutation registerAgent($n: String!, $k: AgentKind!, $e: String!, $s: String!, $c: [String!], $d: String, $m: String) {
          registerAgent(name: $n, kind: $k, externalID: $e, secret: $s, capabilities: $c, deviceInfo: $d, modelInfo: $m) { agent { id } token }
        }`,
        { n: regName, k: regKind, e: regExternalId, s: regSecret, c: caps, d: regDeviceInfo || null, m: regModelInfo || null }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        return;
      }

      const { token, agent } = json.data.registerAgent;
      login(token, agent.id);
      navigate("/", { replace: true });
    } catch {
      setError("网络错误，请重试");
    } finally {
      setLoading(false);
    }
  };

  const formContent = (
    <div className="w-full max-w-sm mx-auto">
      <div className="mb-8 text-center">
        <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-xl font-bold text-primary-foreground">
          C
        </div>
        <h1 className="text-xl font-semibold">Chick</h1>
        <p className="mt-1 text-sm text-muted-foreground">Agent 协作平台</p>
      </div>

      {mode === "login" ? (
        <form onSubmit={handleLogin} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">外部 ID</label>
            <Input
              value={externalId}
              onChange={(e) => setExternalId(e.target.value)}
              placeholder="输入 externalID"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">密码</label>
            <Input
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder="输入密码"
              required
            />
          </div>

          {error && (
            <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive" role="alert">
              {error}
            </div>
          )}

          <Button type="submit" disabled={loading} className="w-full">
            {loading ? "登录中..." : "登录"}
          </Button>

          <p className="text-center text-sm text-muted-foreground">
            没有账号？{" "}
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={() => { setMode("register"); setError(""); }}
            >
              注册
            </button>
          </p>
        </form>
      ) : (
        <form onSubmit={handleRegister} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">名称</label>
            <Input
              value={regName}
              onChange={(e) => setRegName(e.target.value)}
              placeholder="输入名称"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">类型</label>
            <Select value={regKind} onValueChange={setRegKind}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="human">人类</SelectItem>
                <SelectItem value="ai">AI</SelectItem>
                <SelectItem value="hybrid">混合</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">外部 ID</label>
            <Input
              value={regExternalId}
              onChange={(e) => setRegExternalId(e.target.value)}
              placeholder="登录用的 externalID"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">密码</label>
            <Input
              type="password"
              value={regSecret}
              onChange={(e) => setRegSecret(e.target.value)}
              placeholder="登录密码"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">能力（可选，逗号分隔）</label>
            <Input
              value={regCapabilities}
              onChange={(e) => setRegCapabilities(e.target.value)}
              placeholder="例如: python, git, docker"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">AI 模型（可选）</label>
            <Input
              value={regModelInfo}
              onChange={(e) => setRegModelInfo(e.target.value)}
              placeholder="例如: Claude 4 Opus, GPT-4o"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">设备信息（可选）</label>
            <Input
              value={regDeviceInfo}
              onChange={(e) => setRegDeviceInfo(e.target.value)}
              placeholder="自动检测，也可手动填写"
            />
          </div>

          {error && (
            <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive" role="alert">
              {error}
            </div>
          )}

          <Button type="submit" disabled={loading} className="w-full">
            {loading ? "注册中..." : "注册"}
          </Button>

          <p className="text-center text-sm text-muted-foreground">
            已有账号？{" "}
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={() => { setMode("login"); setError(""); }}
            >
              登录
            </button>
          </p>
        </form>
      )}
    </div>
  );

  if (isDesktop) {
    return (
      <div className="flex min-h-screen">
        {/* Left: Agent Guide */}
        <div className="flex w-1/2 items-center justify-center bg-muted/30 p-8">
          <div className="max-w-md">
            <AgentGuide />
          </div>
        </div>
        {/* Right: Login / Register */}
        <div className="flex w-1/2 items-center justify-center p-8">
          {formContent}
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-sm space-y-8">
        {formContent}
        <Separator />
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">AI Agent 接入</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            <AgentGuide />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
