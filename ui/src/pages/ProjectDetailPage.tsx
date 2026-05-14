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

interface Label {
  id: string;
  name: string;
  color: string | null;
}

interface Milestone {
  id: string;
  title: string;
}

interface Issue {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
  assignees: Array<{ agent: { id: string; name: string } }>;
  labels: Label[];
  milestone: Milestone | null;
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
  { state: "later", label: "稍后处理" },
  { state: "reopen", label: "重新打开" },
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
  const [projectLabels, setProjectLabels] = useState<Label[]>([]);
  const [projectMilestones, setProjectMilestones] = useState<Milestone[]>([]);

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    Promise.all([
      gql(`query project($id: ID!) { project(id: $id) { id name description } }`, { id }),
      gql(
        `query issues($projectId: ID!) { issues(projectID: $projectId) { edges { id number title state priority assignees { agent { id name } } labels { id name color } milestone { id title } } } }`,
        { projectId: id }
      ),
      gql(`query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`, { projectId: id }),
      gql(`query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title } }`, { projectId: id }),
    ])
      .then(([pJson, iJson, lJson, mJson]) => {
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (iJson.errors) { setError(iJson.errors[0].message); return; }
        setProject(pJson.data.project);
        setIssues(iJson.data.issues.edges);
        if (!lJson.errors) setProjectLabels(lJson.data.labels);
        if (!mJson.errors) setProjectMilestones(mJson.data.milestones);
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

  const handleAddLabel = async (issueId: string, labelId: string) => {
    const json = await gql(
      `mutation addLabels($issueID: ID!, $labelIDs: [ID!]!) { addLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
      { issueID: issueId, labelIDs: [labelId] }
    );
    if (!json.errors) {
      setIssues((prev) => prev.map((i) => i.id === issueId ? { ...i, labels: json.data.addLabels.labels } : i));
      toast.success("标签已添加");
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleRemoveLabel = async (issueId: string, labelId: string) => {
    const json = await gql(
      `mutation removeLabels($issueID: ID!, $labelIDs: [ID!]!) { removeLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
      { issueID: issueId, labelIDs: [labelId] }
    );
    if (!json.errors) {
      setIssues((prev) => prev.map((i) => i.id === issueId ? { ...i, labels: json.data.removeLabels.labels } : i));
      toast.success("标签已移除");
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleChangeMilestone = async (issueId: string, milestoneId: string) => {
    const json = await gql(
      `mutation updateIssue($id: ID!, $milestoneId: ID) { updateIssue(id: $id, milestoneId: $milestoneId) { id milestone { id title } } }`,
      { id: issueId, milestoneId: milestoneId || null }
    );
    if (!json.errors) {
      setIssues((prev) => prev.map((i) => i.id === issueId ? { ...i, milestone: json.data.updateIssue.milestone } : i));
      toast.success("里程碑已更新");
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleCreateMilestone = async (title: string) => {
    const json = await gql(
      `mutation createMilestone($projectID: ID!, $title: String!) { createMilestone(projectID: $projectID, title: $title) { id title } }`,
      { projectID: id!, title }
    );
    if (!json.errors) {
      setProjectMilestones((prev) => [...prev, json.data.createMilestone]);
      toast.success("里程碑已创建");
      return json.data.createMilestone;
    }
    toast.error(json.errors[0].message);
    return null;
  };

  const handleCreateLabel = async (name: string, color: string) => {
    const json = await gql(
      `mutation createLabel($projectID: ID!, $name: String!, $color: String) { createLabel(projectID: $projectID, name: $name, color: $color) { id name color } }`,
      { projectID: id!, name, color }
    );
    if (!json.errors) {
      setProjectLabels((prev) => [...prev, json.data.createLabel]);
      toast.success("标签已创建");
      return json.data.createLabel;
    }
    toast.error(json.errors[0].message);
    return null;
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
        projectLabels={projectLabels}
        projectMilestones={projectMilestones}
        onAddLabel={handleAddLabel}
        onRemoveLabel={handleRemoveLabel}
        onChangeMilestone={handleChangeMilestone}
        onCreateLabel={handleCreateLabel}
        onCreateMilestone={handleCreateMilestone}
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
