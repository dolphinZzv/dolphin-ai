import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { Bot } from "lucide-react";
import { gql } from "@/lib/graphql";

interface AgentDetail {
  id: string;
  number: number;
  name: string;
  kind: string;
  status: string;
  externalID: string;
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
  const [agent, setAgent] = useState<AgentDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchAgent = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);
    gql(
      `query agent($id: ID!) { agent(id: $id) { id number name kind status externalID capabilities deviceInfo modelInfo lastIP lastSeenAt createdAt } }`,
      { id }
    )
      .then((json) => {
        if (json.errors) { setError(json.errors[0].message); return; }
        setAgent(json.data.agent);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => { fetchAgent(); }, [fetchAgent]);

  if (loading) return <Skeleton className="h-48 w-full" />;
  if (error) return <ErrorFallback message={error} onRetry={fetchAgent} />;
  if (!agent) return <ErrorFallback message="Agent 不存在" />;

  const status = statusConfig[agent.status] || statusConfig.offline;

  return (
    <div className="space-y-4">
      <Link to="/agents" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        ← 返回列表
      </Link>
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
              <p className="text-muted-foreground mb-0.5">外部 ID</p>
              <p className="font-mono">{agent.externalID}</p>
            </div>
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
    </div>
  );
}
