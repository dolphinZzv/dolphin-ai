import { useEffect, useState, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { Bot, ToggleLeft, ToggleRight, AlertTriangle } from "lucide-react";
import { gql } from "@/lib/graphql";
import { toast } from "sonner";

interface AgentDetail {
  id: string;
  number: number;
  name: string;
  kind: string;
  status: string;
  disabled: boolean;
  externalID: string;
  tokenPreview?: string;
  capabilities: string[];
  deviceInfo?: string;
  modelInfo?: string;
  lastIP?: string;
  lastSeenAt?: string;
  createdAt: string;
}

const statusConfig: Record<string, { label: string; dot: string }> = {
  online: { label: "在线", dot: "bg-green-500" },
  busy: { label: "忙碌", dot: "bg-amber-500" },
  offline: { label: "离线", dot: "bg-gray-400" },
  error: { label: "错误", dot: "bg-red-500" },
};

const kindLabels: Record<string, string> = {
  ai: "AI",
  human: "人类",
  hybrid: "混合",
};

export function AgentDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [agent, setAgent] = useState<AgentDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toggling, setToggling] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const fetchAgent = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);
    gql(
      `query agent($id: ID!) { agent(id: $id) { id number name kind status disabled externalID tokenPreview capabilities deviceInfo modelInfo lastIP lastSeenAt createdAt } }`,
      { id }
    )
      .then((json) => {
        if (json.errors) { setError(json.errors[0].message); return; }
        setAgent(json.data.agent);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  const toggleDisabled = useCallback(() => {
    if (!id || !agent || toggling) return;
    const action = agent.disabled ? "启用" : "禁用";
    if (!window.confirm(`确认${action} Agent #${agent.number} ${agent.name}？`)) return;
    setToggling(true);
    const newDisabled = !agent.disabled;
    gql(
      `mutation updateAgentDisabled($id: ID!, $disabled: Boolean!) { updateAgentDisabled(id: $id, disabled: $disabled) { id disabled } }`,
      { id, disabled: newDisabled }
    )
      .then((json) => {
        if (json.errors) { return; }
        setAgent((prev) => prev ? { ...prev, disabled: newDisabled } : prev);
      })
      .finally(() => setToggling(false));
  }, [id, agent, toggling]);

  const handleDelete = useCallback(async () => {
    if (!id || !window.confirm(`确认删除 Agent #${agent?.number} ${agent?.name}？此操作不可撤销。`)) return;
    setDeleting(true);
    try {
      const json = await gql(`mutation deleteAgent($id: ID!) { deleteAgent(id: $id) }`, { id });
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("Agent 已删除");
      navigate("/projects", { replace: true });
    } catch {
      toast.error("网络错误");
    } finally {
      setDeleting(false);
    }
  }, [id, agent, navigate]);

  useEffect(() => { fetchAgent(); }, [fetchAgent]);

  if (loading) return <Skeleton className="h-48 w-full" />;
  if (error) return <ErrorFallback message={error} onRetry={fetchAgent} />;
  if (!agent) return <ErrorFallback message="Agent 不存在" />;

  const status = statusConfig[agent.status] || statusConfig.offline;

  return (
    <div className="space-y-4">
      <button onClick={() => navigate(-1)} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        ← 返回
      </button>
      <Card>
        <CardContent className="p-6">
          <div className="flex items-center gap-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
              <Bot className="h-8 w-8 text-muted-foreground" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-semibold">#{agent.number} {agent.name}</h1>
                <div className={`h-3 w-3 rounded-full ${status.dot}`} />
              </div>
              <p className="text-sm text-muted-foreground mt-1">
                {kindLabels[agent.kind] || agent.kind} · {status.label}
              </p>
            </div>
            <button
              onClick={toggleDisabled}
              disabled={toggling}
              className={`inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                agent.disabled
                  ? "bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400"
                  : "bg-green-100 text-green-700 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-400"
              }`}
              title={agent.disabled ? "点击启用" : "点击禁用"}
            >
              {agent.disabled ? <ToggleLeft className="h-5 w-5" /> : <ToggleRight className="h-5 w-5" />}
              {agent.disabled ? "已禁用" : "已启用"}
            </button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-6 space-y-4">
          <h2 className="text-lg font-semibold">基本信息</h2>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <p className="text-muted-foreground mb-0.5">编号</p>
              <p className="font-mono">#{agent.number}</p>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">ID</p>
              <p className="font-mono">{agent.id}</p>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">账户</p>
              <p className="font-mono">{agent.externalID}</p>
            </div>
            {agent.tokenPreview && (
            <div>
              <p className="text-muted-foreground mb-0.5">Token</p>
              <p className="font-mono text-xs tracking-wider">{agent.tokenPreview}</p>
            </div>
            )}
            <div>
              <p className="text-muted-foreground mb-0.5">类型</p>
              <p>{kindLabels[agent.kind] || agent.kind}</p>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">状态</p>
              <div className="flex items-center gap-1.5">
                <div className={`h-2.5 w-2.5 rounded-full ${status.dot}`} />
                <span>{status.label}</span>
              </div>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">IP 地址</p>
              <p className="font-mono">{agent.lastIP || "未知"}</p>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">注册时间</p>
              <p>{new Date(agent.createdAt).toLocaleString("zh-CN")}</p>
            </div>
            <div>
              <p className="text-muted-foreground mb-0.5">最后活跃</p>
              <p>{agent.lastSeenAt ? new Date(agent.lastSeenAt).toLocaleString("zh-CN") : "未知"}</p>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-6">
          <h2 className="text-lg font-semibold mb-3">能力</h2>
          {!agent.capabilities || agent.capabilities.length === 0 ? (
            <p className="text-sm text-muted-foreground">暂无能力</p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {agent.capabilities.map((cap) => (
                <Badge key={cap} variant="secondary">{cap}</Badge>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-6 space-y-3">
          <h2 className="text-lg font-semibold">设备与模型</h2>
          <div>
            <p className="text-sm text-muted-foreground mb-0.5">AI 模型</p>
            <p className="text-sm font-mono">{agent.modelInfo || "未提供"}</p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground mb-0.5">设备信息</p>
            <p className="text-sm font-mono whitespace-pre-wrap">{agent.deviceInfo || "未提供"}</p>
          </div>
        </CardContent>
      </Card>

      {/* Danger Zone */}
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4">
        <div className="flex items-center gap-2 mb-3">
          <AlertTriangle className="h-5 w-5 text-destructive" />
          <h2 className="text-base font-semibold text-destructive">危险区域</h2>
        </div>
        <p className="text-sm text-muted-foreground mb-3">删除 Agent 后不可恢复。相关的评论和分配记录将被保留，但 Agent 账户将被永久删除。</p>
        <Button variant="destructive" size="sm" onClick={handleDelete} disabled={deleting}>
          {deleting ? "删除中..." : "删除 Agent"}
        </Button>
      </div>
    </div>
  );
}
