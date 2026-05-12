import { useEffect, useState, useCallback } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { gql } from "@/lib/graphql";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { FolderKanban, CircleDot, Activity, Plus } from "lucide-react";

interface ProjectBrief {
  id: string;
  name: string;
  description: string;
}

interface AgentBrief {
  id: string;
  name: string;
  status: string;
  kind: string;
}

interface Stats {
  totalProjects: number;
  onlineAgents: number;
}

const statusConfig: Record<string, { label: string; color: string }> = {
  online: { label: "在线", color: "bg-green-500" },
  busy: { label: "忙碌", color: "bg-amber-500" },
  offline: { label: "离线", color: "bg-gray-400" },
  error: { label: "错误", color: "bg-red-500" },
};

export function DashboardPage() {
  const [projects, setProjects] = useState<ProjectBrief[]>([]);
  const [agents, setAgents] = useState<AgentBrief[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newDesc, setNewDesc] = useState("");
  const [creating, setCreating] = useState(false);

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);

    Promise.all([
      gql(`query projects { projects { id name description } }`),
      gql(`query agents { agents { id name status kind } }`),
    ])
      .then(([pJson, aJson]) => {
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (aJson.errors) { setError(aJson.errors[0].message); return; }
        setProjects(pJson.data.projects);
        setAgents(aJson.data.agents);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { fetchData(); }, [fetchData]);

  if (loading) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-semibold">仪表盘</h1>
        <div className="grid gap-3 sm:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <Card key={i}><CardHeader><Skeleton className="h-4 w-20" /></CardHeader><CardContent><Skeleton className="h-8 w-16" /></CardContent></Card>
          ))}
        </div>
        <div className="grid gap-3 lg:grid-cols-2">
          {[1, 2, 3].map((i) => <Skeleton key={i} className="h-24 w-full" />)}
        </div>
      </div>
    );
  }

  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;

  const statusCounts = { online: 0, busy: 0, offline: 0, error: 0 };
  agents.forEach((a) => {
    if (a.status in statusCounts) statusCounts[a.status as keyof typeof statusCounts]++;
  });

  const stats: Stats = {
    totalProjects: projects.length,
    onlineAgents: agents.filter((a) => a.status === "online").length,
  };

  const statCards = [
    { title: "项目数", value: stats.totalProjects, icon: FolderKanban },
    { title: "在线 Agent", value: stats.onlineAgents, icon: CircleDot },
    { title: "Agent 总数", value: agents.length, icon: Activity },
  ];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">仪表盘</h1>

      {/* StatsCards */}
      <div className="grid gap-3 sm:grid-cols-3">
        {statCards.map((stat) => (
          <Card key={stat.title}>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">
                {stat.title}
              </CardTitle>
              <stat.icon className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stat.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* AgentStatusSummary */}
      {agents.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Agent 状态
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-3">
              {(Object.entries(statusConfig) as [string, { label: string; color: string }][]).map(
                ([key, cfg]) => {
                  const count = statusCounts[key as keyof typeof statusCounts];
                  if (count === 0) return null;
                  return (
                    <div key={key} className="flex items-center gap-2">
                      <div className={`h-2.5 w-2.5 rounded-full ${cfg.color}`} />
                      <span className="text-sm text-muted-foreground">{cfg.label}</span>
                      <Badge variant="secondary" className="text-xs">{count}</Badge>
                    </div>
                  );
                }
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Create project dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建项目</DialogTitle>
          </DialogHeader>
          <form onSubmit={async (e) => {
            e.preventDefault();
            if (!newName.trim()) return;
            setCreating(true);
            try {
              const json = await gql(
                `mutation createProject($name: String!, $desc: String) {
                  createProject(name: $name, description: $desc) { id name }
                }`,
                { name: newName, desc: newDesc || null }
              );
              if (json.errors) { toast.error(json.errors[0].message); return; }
              toast.success("项目创建成功");
              setCreateOpen(false);
              setNewName("");
              setNewDesc("");
              fetchData();
            } catch {
              toast.error("网络错误");
            } finally {
              setCreating(false);
            }
          }} className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">项目名称</label>
              <Input value={newName} onChange={e => setNewName(e.target.value)} placeholder="输入项目名称" required />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">描述（可选）</label>
              <Input value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="项目描述" />
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
              <Button type="submit" disabled={creating}>{creating ? "创建中..." : "创建"}</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      {/* Project list */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-medium">项目</h2>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="mr-1 h-4 w-4" />
            创建项目
          </Button>
        </div>
        {projects.length === 0 ? (
          <EmptyState title="暂无项目" description="创建一个项目开始使用" />
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {projects.map((p) => (
              <Link
                key={p.id}
                to={`/projects/${p.id}`}
                className="block rounded-lg border bg-card p-4 transition-colors hover:bg-accent"
              >
                <h3 className="font-medium">{p.name}</h3>
                {p.description && (
                  <p className="mt-1 text-sm text-muted-foreground line-clamp-2">{p.description}</p>
                )}
              </Link>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
