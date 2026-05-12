import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { gql } from "@/lib/graphql";
import { useAuth } from "@/hooks/useAuth";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { CreateIssueDialog } from "@/components/project/CreateIssueDialog";
import { IssueBoard } from "@/components/project/IssueBoard";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { toast } from "sonner";

interface Issue {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
}

interface Project {
  id: string;
  name: string;
  description: string;
}

const columns = [
  { state: "open", label: "待处理" },
  { state: "in_progress", label: "进行中" },
  { state: "blocked", label: "阻塞" },
  { state: "review", label: "审查" },
];

const priorityLabels: Record<string, string> = {
  critical: "关键",
  high: "高",
  medium: "中",
  low: "低",
};

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { agent } = useAuth();
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  const [project, setProject] = useState<Project | null>(null);
  const [issues, setIssues] = useState<Issue[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState("");
  const [priorityFilter, setPriorityFilter] = useState("all");

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    Promise.all([
      gql(`query project($id: ID!) { project(id: $id) { id name description } }`, { id }),
      gql(
        `query issues($projectId: ID!) { issues(projectID: $projectId) { edges { id number title state priority } } }`,
        { projectId: id }
      ),
    ])
      .then(([pJson, iJson]) => {
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (iJson.errors) { setError(iJson.errors[0].message); return; }
        setProject(pJson.data.project);
        setIssues(iJson.data.issues.edges);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleTransition = async (issueId: string, toState: string) => {
    const json = await gql(
      `mutation transitionIssue($id: ID!, $newState: IssueState!, $actorId: ID!) {
        transitionIssue(id: $id, newState: $newState, actorID: $actorId) { state }
      }`,
      { id: issueId, newState: toState, actorId: agent?.agentId || "" }
    );
    if (json.errors) {
      toast.error(json.errors[0].message);
      throw new Error(json.errors[0].message);
    }
    toast.success("状态变更成功");
    fetchData();
  };

  const filteredIssues = issues.filter((issue) => {
    if (search && !issue.title.toLowerCase().includes(search.toLowerCase())) return false;
    if (priorityFilter !== "all" && issue.priority !== priorityFilter) return false;
    return true;
  });

  const activeIssues = filteredIssues.filter((i) => !i.state.startsWith("closed"));
  const closed = filteredIssues.filter((i) => i.state.startsWith("closed"));

  if (loading) return (
    <div className="space-y-4">
      <Skeleton className="h-8 w-48" />
      <div className="grid gap-3 lg:grid-cols-4">
        {columns.map((c) => (
          <div key={c.state} className="space-y-2">
            <Skeleton className="h-6 w-20" />
            {[1, 2].map((i) => <Skeleton key={i} className="h-20 w-full" />)}
          </div>
        ))}
      </div>
    </div>
  );

  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;
  if (!project) return <EmptyState title="项目不存在" />;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-xl sm:text-2xl font-semibold">{project.name}</h1>
          {project.description && (
            <p className="mt-1 text-sm text-muted-foreground">{project.description}</p>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Link to={`/projects/${id}/settings`}>
            <Button variant="outline" size="sm" className="text-xs sm:text-sm">设置</Button>
          </Link>
          {agent && (
            <CreateIssueDialog
              projectId={id!}
              onCreated={fetchData}
            />
          )}
        </div>
      </div>

      {/* FilterBar */}
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder="搜索 Issue..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-9 w-full sm:w-60"
        />
        <Select value={priorityFilter} onValueChange={setPriorityFilter}>
          <SelectTrigger className="h-9 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部优先级</SelectItem>
            {Object.entries(priorityLabels).map(([v, l]) => (
              <SelectItem key={v} value={v}>{l}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Kanban board with dnd-kit */}
      <IssueBoard
        columns={columns}
        issues={activeIssues}
        isDesktop={isDesktop}
        onTransition={handleTransition}
      />

      {/* Closed issues */}
      {closed.length > 0 && (
        <details className="rounded-lg border bg-card">
          <summary className="cursor-pointer px-4 py-2 text-sm font-medium text-muted-foreground">
            已关闭 ({closed.length})
          </summary>
          <div className="space-y-1 px-4 pb-3">
            {closed.map((issue) => (
              <Link key={issue.id} to={`/issues/${issue.id}`} className="block rounded px-2 py-1 text-sm hover:bg-accent">
                <span className="text-muted-foreground line-through">#{issue.number}</span>{" "}
                <span className="text-muted-foreground line-through">{issue.title}</span>
              </Link>
            ))}
          </div>
        </details>
      )}
    </div>
  );
}
