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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { ChevronDown } from "lucide-react";
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
  { state: "pending_confirmation", label: "待确认" },
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

  const [searchInput, setSearchInput] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [labelFilter, setLabelFilter] = useState<string[]>([]);
  const [milestoneFilter, setMilestoneFilter] = useState("all");
  const [assigneeFilter, setAssigneeFilter] = useState("all");
  const [projectLabels, setProjectLabels] = useState<Label[]>([]);
  const [projectMilestones, setProjectMilestones] = useState<Milestone[]>([]);
  const [validTransitions, setValidTransitions] = useState<Record<string, string[]>>({});

  // Debounce search input before sending to backend
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(searchInput), 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    const issueVars: Record<string, unknown> = { projectId: id };
    if (debouncedSearch) issueVars.search = debouncedSearch;
    if (priorityFilter !== "all") issueVars.priority = priorityFilter;
    if (labelFilter.length > 0) issueVars.labelIDs = labelFilter;
    if (assigneeFilter !== "all") issueVars.assigneeID = assigneeFilter;

    Promise.all([
      gql(`query project($id: ID!) { project(id: $id) { id name description } }`, { id }),
      gql(
        `query issues($projectId: ID!, $search: String, $priority: Priority, $labelIDs: [ID!], $assigneeID: ID) {
          issues(projectID: $projectId, search: $search, priority: $priority, labelIDs: $labelIDs, assigneeID: $assigneeID) {
            edges { id number title state priority assignees { agent { id name } } labels { id name color } milestone { id title } }
          }
        }`,
        issueVars
      ),
      gql(`query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`, { projectId: id }),
      gql(`query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title } }`, { projectId: id }),
      ...columns.map((c) =>
        gql(`query validTransitions($state: IssueState!) { validTransitions(state: $state) }`, { state: c.state })
      ),
    ])
      .then((results) => {
        const pJson = results[0]; const iJson = results[1]; const lJson = results[2]; const mJson = results[3];
        const transitionResults = results.slice(4) as Array<{ data?: { validTransitions: string[] }; errors?: any }>;
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (iJson.errors) { setError(iJson.errors[0].message); return; }
        setProject(pJson.data.project);
        setIssues(iJson.data.issues.edges);
        if (!lJson.errors) setProjectLabels(lJson.data.labels);
        if (!mJson.errors) setProjectMilestones(mJson.data.milestones);
        const vt: Record<string, string[]> = {};
        transitionResults.forEach((r, i) => {
          if (r.data?.validTransitions) vt[columns[i].state] = r.data.validTransitions;
        });
        setValidTransitions(vt);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id, debouncedSearch, priorityFilter, labelFilter, assigneeFilter]);

  useEffect(() => { fetchData(); }, [fetchData]);

  // Auto-refresh when tab becomes visible (covers background updates)
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") fetchData();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [fetchData]);

  const handleTransition = async (issueId: string, toState: string, note?: string) => {
    const targetLabel = columns.find((c) => c.state === toState)?.label || toState;
    if (!window.confirm(`确认将该 Issue 状态变更为「${targetLabel}」？`)) return;
    const json = await gql(
      `mutation transitionIssue($id: ID!, $newState: IssueState!, $actorId: ID!, $note: String) {
        transitionIssue(id: $id, newState: $newState, actorID: $actorId, note: $note) { state }
      }`,
      { id: issueId, newState: toState, actorId: agent?.agentId || "", note: note || null }
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
    if (!window.confirm("确认从该 Issue 中移除该标签？")) return;
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

  // Milestone filter is applied client-side (backend doesn't support it directly)
  const filteredIssues = issues.filter((issue) => {
    if (milestoneFilter !== "all") {
      if (!issue.milestone || issue.milestone.id !== milestoneFilter) return false;
    }
    return true;
  });

  // Extract unique assignees from all fetched issues
  const allAssignees = issues.reduce<Array<{ id: string; name: string }>>((acc, issue) => {
    (issue.assignees || []).forEach((a) => {
      if (!acc.some((x) => x.id === a.agent.id)) acc.push(a.agent);
    });
    return acc;
  }, []);

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
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
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

        {/* Label filter */}
        <Popover>
          <PopoverTrigger asChild>
            <Button variant="outline" size="sm" className="h-9 gap-1 text-xs">
              标签{labelFilter.length > 0 ? ` (${labelFilter.length})` : ""}
              <ChevronDown className="h-3 w-3 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-48 p-2" align="start">
            <div className="space-y-1">
              {projectLabels.length === 0 && <p className="text-xs text-muted-foreground">暂无标签</p>}
              {projectLabels.map((l) => (
                <label key={l.id} className="flex items-center gap-2 rounded px-1 py-1 text-xs hover:bg-accent cursor-pointer">
                  <Checkbox
                    checked={labelFilter.includes(l.id)}
                    onCheckedChange={(checked) => {
                      setLabelFilter(checked ? [...labelFilter, l.id] : labelFilter.filter((id) => id !== l.id));
                    }}
                  />
                  <span className="truncate" style={{ color: l.color || undefined }}>{l.name}</span>
                </label>
              ))}
            </div>
          </PopoverContent>
        </Popover>

        {/* Milestone filter */}
        <Select value={milestoneFilter} onValueChange={setMilestoneFilter}>
          <SelectTrigger className="h-9 w-36">
            <SelectValue placeholder="里程碑" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部里程碑</SelectItem>
            {projectMilestones.map((m) => (
              <SelectItem key={m.id} value={m.id}>{m.title}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Assignee filter */}
        <Select value={assigneeFilter} onValueChange={setAssigneeFilter}>
          <SelectTrigger className="h-9 w-36">
            <SelectValue placeholder="指派对象" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部成员</SelectItem>
            {allAssignees.map((a) => (
              <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
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
        validTransitions={validTransitions}
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
