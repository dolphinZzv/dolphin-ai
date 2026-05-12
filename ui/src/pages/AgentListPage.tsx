import { useEffect, useState, useCallback } from "react";
import { useSearchParams, Link } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { Bot } from "lucide-react";

interface Agent {
  id: string;
  name: string;
  kind: string;
  status: string;
  capabilities: string[];
  deviceInfo?: string;
  modelInfo?: string;
}

const statusConfig: Record<string, { label: string; dot: string; badge: string }> = {
  online: { label: "在线", dot: "bg-green-500", badge: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200" },
  busy: { label: "忙碌", dot: "bg-amber-500", badge: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200" },
  offline: { label: "离线", dot: "bg-gray-400", badge: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200" },
  error: { label: "错误", dot: "bg-red-500", badge: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200" },
};

const kindLabels: Record<string, string> = {
  ai: "AI",
  human: "人类",
  hybrid: "混合",
};

export function AgentListPage() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState("all");
  const [kindFilter, setKindFilter] = useState("all");
  const [searchParams] = useSearchParams();
  const searchQuery = searchParams.get("search") || "";

  const token = typeof window !== "undefined" ? localStorage.getItem("token") : null;
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);
    fetch("/graphql", {
      method: "POST",
      headers,
      body: JSON.stringify({
        operationName: "agents",
        query: `query agents { agents { id name kind status capabilities deviceInfo modelInfo } }`,
      }),
    })
      .then((r) => r.json())
      .then((json) => {
        if (json.errors) { setError(json.errors[0].message); return; }
        setAgents(json.data.agents);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { fetchData(); }, [fetchData]);

  const filtered = agents.filter((a) => {
    if (statusFilter !== "all" && a.status !== statusFilter) return false;
    if (kindFilter !== "all" && a.kind !== kindFilter) return false;
    if (searchQuery && !a.name.toLowerCase().includes(searchQuery.toLowerCase())) return false;
    return true;
  });

  if (loading) return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Agent</h1>
      <div className="grid gap-3 lg:grid-cols-2">
        {[1, 2, 3].map((i) => <Skeleton key={i} className="h-24 w-full" />)}
      </div>
    </div>
  );

  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Agent</h1>

      {/* FilterBar */}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="h-9 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {Object.entries(statusConfig).map(([v, c]) => (
              <SelectItem key={v} value={v}>{c.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={kindFilter} onValueChange={setKindFilter}>
          <SelectTrigger className="h-9 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部类型</SelectItem>
            {Object.entries(kindLabels).map(([v, l]) => (
              <SelectItem key={v} value={v}>{l}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          icon={<Bot className="h-12 w-12" />}
          title="暂无 Agent"
          description="没有匹配的 Agent"
        />
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {filtered.map((agent) => {
            const status = statusConfig[agent.status] || statusConfig.OFFLINE;
            return (
              <Link key={agent.id} to={`/agents/${agent.id}`} className="block">
              <Card>
                <CardContent className="p-4">
                  <div className="flex items-center gap-3">
                    <div className={`h-3 w-3 rounded-full ${status.dot}`} />
                    <div className="flex-1 min-w-0">
                      <p className="font-medium truncate">{agent.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {kindLabels[agent.kind] || agent.kind}
                      </p>
                    </div>
                    <Badge className={`text-xs ${status.badge}`}>
                      {status.label}
                    </Badge>
                  </div>
                  {(agent.deviceInfo || agent.modelInfo) && (
                    <div className="mt-2 text-xs text-muted-foreground space-y-0.5">
                      {agent.modelInfo && <p>模型: {agent.modelInfo}</p>}
                      {agent.deviceInfo && <p className="truncate" title={agent.deviceInfo}>设备: {agent.deviceInfo}</p>}
                    </div>
                  )}
                  {agent.capabilities.length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1">
                      {agent.capabilities.map((cap) => (
                        <Badge key={cap} variant="secondary" className="text-xs">
                          {cap}
                        </Badge>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
