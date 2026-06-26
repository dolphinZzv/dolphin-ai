import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { gql } from "@/lib/graphql";
import { MarkdownContent } from "@/components/shared/MarkdownContent";

import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Separator } from "@/components/ui/separator";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Send, MessageSquare, Trash2, Plus, Pencil, X, Check } from "lucide-react";
import { toast } from "sonner";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { useSubscription } from "@/hooks/useSubscription";

interface IssueDetail {
  id: string;
  number: number;
  title: string;
  description: string | null;
  state: string;
  priority: string;
  dueDate: string | null;
  startedAt: string | null;
  completedAt: string | null;
  difficulty: number | null;
  environment: string | null;
  branch: string | null;
  link: string | null;
  createdAt: string;
  creator: { id: string; name: string };
  assignees: Array<{
    id: string;
    agent: { id: string; name: string };
    state: string;
  }>;
  labels: Array<{ id: string; name: string; color: string | null }>;
  milestone: { id: string; title: string } | null;
  projectID: string;
}

interface Comment {
  id: string;
  body: string;
  createdAt: string;
  author: { id: string; name: string };
  parentID?: string | null;
  replies?: Comment[];
}

interface TimelineEvent {
  id: string;
  eventType: string;
  createdAt: string;
  actor: { id: string; name: string };
  payload: Record<string, unknown> | null;
}

interface Label {
  id: string;
  name: string;
  color: string | null;
}

interface Milestone {
  id: string;
  title: string;
  state: string;
}

const stateLabels: Record<string, string> = {
  open: "待处理",
  in_progress: "进行中",
  blocked: "阻塞",
  review: "审查",
  pending_confirmation: "待确认",
  later: "稍后处理",
  closed_completed: "已完成",
  closed_not_planned: "已关闭",
  closed_rejected: "已拒绝",
  reopen: "重新打开",
};

const stateBadgeColors: Record<string, string> = {
  open: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  in_progress: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  blocked: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  review: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200",
  pending_confirmation: "bg-cyan-100 text-cyan-800 dark:bg-cyan-900 dark:text-cyan-200",
  later: "bg-slate-100 text-slate-800 dark:bg-slate-900 dark:text-slate-200",
  reopen: "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200",
  closed_completed: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200",
  closed_not_planned: "bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400",
  closed_rejected: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
};

const priorityLabels: Record<string, string> = {
  critical: "关键",
  high: "高",
  medium: "中",
  low: "低",
};

const eventTypeLabels: Record<string, string> = {
  issue_created: "创建 Issue",
  issue_updated: "更新 Issue",
  issue_transitioned: "状态变更",
  assignee_added: "分配",
  assignee_removed: "移除分配",
  comment_added: "评论",
  label_added: "添加标签",
  label_removed: "移除标签",
};

const eventIcons: Record<string, string> = {
  issue_created: "●",
  issue_transitioned: "→",
  assignee_added: "👤",
  comment_added: "💬",
  label_added: "🏷",
};

function relativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin} 分钟前`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;
  const days = Math.floor(diffHour / 24);
  if (days === 1) return "昨天";
  if (days < 7) return `${days} 天前`;
  return dateStr.slice(0, 10);
}

function Timeline({ events }: { events: TimelineEvent[] }) {
  return (
    <div className="space-y-0">
      {events.map((event, idx) => {
        const note = event.payload && typeof event.payload === "object" && "note" in event.payload
          ? String(event.payload.note)
          : null;
        const fromState = event.payload && typeof event.payload === "object" && "from" in event.payload
          ? String(event.payload.from)
          : null;
        const toState = event.payload && typeof event.payload === "object" && "to" in event.payload
          ? String(event.payload.to)
          : null;
        const transitionDetail = event.eventType === "state_changed" && fromState && toState
          ? `${stateLabels[fromState] || fromState} → ${stateLabels[toState] || toState}`
          : null;
        return (
        <div key={event.id} className="flex gap-3">
          <div className="flex flex-col items-center">
            <div className="mt-1.5 flex h-5 w-5 items-center justify-center rounded-full border bg-card text-[10px] text-muted-foreground">
              {eventIcons[event.eventType] || "●"}
            </div>
            {idx < events.length - 1 && (
              <div className="w-px flex-1 bg-border" />
            )}
          </div>
          <div className="flex-1 pb-4">
            <div className="flex items-baseline gap-2">
              <span className="text-sm font-medium">{event.actor.name}</span>
              <span className="text-xs text-muted-foreground">
                {eventTypeLabels[event.eventType] || event.eventType}
              </span>
              <span className="text-xs text-muted-foreground">
                {relativeTime(event.createdAt)}
              </span>
            </div>
            {transitionDetail && (
              <p className="text-xs text-muted-foreground mt-0.5">{transitionDetail}</p>
            )}
            {note && (
              <p className="mt-1 text-xs bg-muted/50 rounded px-2 py-1 italic">{note}</p>
            )}
          </div>
        </div>
        );
      })}
    </div>
  );
}

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { agent } = useAuth();
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  const [issue, setIssue] = useState<IssueDetail | null>(null);
  const [comments, setComments] = useState<Comment[]>([]);
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newComment, setNewComment] = useState("");
  const [projectLabels, setProjectLabels] = useState<Label[]>([]);
  const [projectMilestones, setProjectMilestones] = useState<Milestone[]>([]);
  const [showLabelPicker, setShowLabelPicker] = useState(false);
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [replyText, setReplyText] = useState("");
  const [previewComment, setPreviewComment] = useState(false);
  const [previewReply, setPreviewReply] = useState(false);
  const [editingTitle, setEditingTitle] = useState(false);
  const [editingDescription, setEditingDescription] = useState(false);
  const [editTitle, setEditTitle] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editPriority, setEditPriority] = useState(false);
  const [editDifficulty, setEditDifficulty] = useState(false);
  const [showTimeFields, setShowTimeFields] = useState(false);
  const [editStartedAt, setEditStartedAt] = useState("");
  const [editCompletedAt, setEditCompletedAt] = useState("");
  const [editingEnv, setEditingEnv] = useState(false);
  const [editEnv, setEditEnv] = useState("");
  const [editingBranch, setEditingBranch] = useState(false);
  const [editBranch, setEditBranch] = useState("");
  const [projectAgents, setProjectAgents] = useState<Array<{ id: string; name: string }>>([]);
  const [showAssigneePicker, setShowAssigneePicker] = useState(false);
  const [transitions, setTransitions] = useState<string[]>([]);
  const [showNewMilestone, setShowNewMilestone] = useState(false);
  const [newMilestoneTitle, setNewMilestoneTitle] = useState("");
  const [showNewLabel, setShowNewLabel] = useState(false);
  const [newLabelName, setNewLabelName] = useState("");
  const [newLabelColor, setNewLabelColor] = useState("#6366f1");

  const fetchData = useCallback((showLoading = true) => {
    if (!id) return;
    if (showLoading) setLoading(true);
    setError(null);

    Promise.all([
      gql(
        `query issue($id: ID!) {
          issue(id: $id) {
            id number title description state priority dueDate
            startedAt completedAt difficulty environment branch link
            createdAt
            creator { id name }
            assignees { id agent { id name } state }
            labels { id name color }
            milestone { id title }
            projectID
          }
        }`,
        { id }
      ),
      gql(
        `query comments($issueId: ID!) {
          comments(issueID: $issueId) { id body createdAt parentID author { id name } replies { id body createdAt author { id name } } }
        }`,
        { issueId: id }
      ),
      gql(
        `query timeline($issueId: ID!) {
          timeline(issueID: $issueId) { id eventType createdAt actor { id name } payload }
        }`,
        { issueId: id }
      ),
    ])
      .then(([iJson, cJson, tJson]) => {
        if (iJson.errors) { setError(iJson.errors[0].message); return; }
        if (cJson.errors) { setError(cJson.errors[0].message); return; }
        if (tJson.errors) { setError(tJson.errors[0].message); return; }
        setIssue(iJson.data.issue);
        setComments((cJson.data.comments || []).map((c: Comment) => ({
          ...c,
          replies: [...(c.replies || [])].sort(
            (a: Comment, b: Comment) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
          ),
        })).sort(
          (a: Comment, b: Comment) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
        ));
        setEvents(tJson.data.timeline);

        // Fetch valid transitions for the current state
        const issueState = iJson.data.issue.state;
        gql(
          `query vt($state: IssueState!) { validTransitions(state: $state) }`,
          { state: issueState }
        ).then((vJson) => {
          if (!vJson.errors) setTransitions(vJson.data.validTransitions);
        });

        // Fetch project labels and milestones
        const pid = iJson.data.issue.projectID;
        if (pid) {
          gql(
            `query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`,
            { projectId: pid }
          ).then((lJson) => {
            if (!lJson.errors) setProjectLabels(lJson.data.labels);
          });
          gql(
            `query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title state } }`,
            { projectId: pid }
          ).then((mJson) => {
            if (!mJson.errors) setProjectMilestones(mJson.data.milestones);
          });
        }
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => { fetchData(); }, [fetchData]);

  // Subscribe to real-time issue updates
  useSubscription(
    `subscription issueUpdated($issueID: ID!) {
      issueUpdated(issueID: $issueID) { id number title description state priority dueDate startedAt completedAt difficulty environment branch link createdAt closedAt creator { id name } assignees { id agent { id name } state } labels { id name color } milestone { id title } projectID }
    }`,
    id ? { issueID: id } : undefined,
    (data: any) => {
      if (data?.issueUpdated) {
        setIssue((prev) => prev ? { ...prev, ...data.issueUpdated } : prev);
        fetchData(false);
      }
    },
  );

  const handleTransition = async (toState: string) => {
    if (!id || !agent) return;
    const targetLabel = stateLabels[toState] || toState;
    const note = window.prompt(`请输入变更为「${targetLabel}」的备注说明（可选）：`);
    if (note === null) return;
    try {
      const json = await gql(
        `mutation transitionIssue($id: ID!, $newState: IssueState!, $actorId: ID!, $note: String) {
          transitionIssue(id: $id, newState: $newState, actorID: $actorId, note: $note) { state }
        }`,
        { id, newState: toState, actorId: agent.agentId, note: note || null }
      );
      if (!json.errors && json.data) {
        setIssue((prev) => prev ? { ...prev, state: json.data.transitionIssue.state } : prev);
        toast.success(`状态已变更为 ${stateLabels[toState]}`);
        fetchData(false);
      } else {
        toast.error(json.errors?.[0]?.message || "状态变更失败");
      }
    } catch {
      toast.error("网络错误，状态变更失败");
    }
  };

  const handleDelete = async () => {
    if (!id || !window.confirm("确定删除这个 Issue？此操作不可撤销。")) return;
    try {
      const json = await gql(
        `mutation deleteIssue($id: ID!) { deleteIssue(id: $id) }`,
        { id }
      );
      if (!json.errors) {
        toast.success("Issue 已删除");
        if (issue?.projectID) window.location.href = `/projects/${issue.projectID}`;
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，删除失败");
    }
  };

  const handleComment = async (parentID?: string) => {
    const text = parentID ? replyText : newComment;
    if (!text.trim() || !id || !agent) return;
    try {
      await gql(
        `mutation addComment($issueID: ID!, $authorID: ID!, $body: String!, $contentType: CommentContentType!, $parentID: ID) {
          addComment(issueID: $issueID, authorID: $authorID, body: $body, contentType: $contentType, parentID: $parentID) { id body createdAt author { id name } parentID replies { id body createdAt author { id name } } }
        }`,
        {
          issueID: id,
          authorID: agent.agentId,
          body: text,
          contentType: "markdown",
          parentID: parentID || null,
        }
      );
      if (parentID) { setReplyText(""); setReplyingTo(null); }
      else setNewComment("");
      fetchData(false);
      toast.success("评论发送成功");
    } catch {
      toast.error("网络错误，评论发送失败");
    }
  };

  const handleAddLabel = async (labelId: string) => {
    if (!id) return;
    try {
      const json = await gql(
        `mutation addLabels($issueID: ID!, $labelIDs: [ID!]!) { addLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
        { issueID: id, labelIDs: [labelId] }
      );
      if (!json.errors) {
        setIssue((prev) => prev ? { ...prev, labels: json.data.addLabels.labels } : prev);
        toast.success("标签已添加");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，标签操作失败");
    }
  };

  const handleRemoveLabel = async (labelId: string, labelName?: string) => {
    if (!id) return;
    if (!window.confirm(`确认从 Issue 中移除标签「${labelName || labelId}」？`)) return;
    try {
      const json = await gql(
        `mutation removeLabels($issueID: ID!, $labelIDs: [ID!]!) { removeLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
        { issueID: id, labelIDs: [labelId] }
      );
      if (!json.errors) {
        setIssue((prev) => prev ? { ...prev, labels: json.data.removeLabels.labels } : prev);
        toast.success("标签已移除");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，标签操作失败");
    }
  };

  const handleChangeMilestone = async (milestoneId: string) => {
    if (!id) return;
    try {
      const json = await gql(
        `mutation updateIssue($id: ID!, $milestoneId: ID) {
          updateIssue(id: $id, milestoneId: $milestoneId) { id milestone { id title } }
        }`,
        { id, milestoneId: milestoneId || null }
      );
      if (!json.errors && json.data) {
        setIssue((prev) => prev ? { ...prev, milestone: json.data.updateIssue.milestone } : prev);
        toast.success("里程碑已更新");
      } else {
        toast.error(json.errors?.[0]?.message || "更新失败");
      }
    } catch {
      toast.error("网络错误，更新失败");
    }
  };

  const handleUpdateIssue = async (fields: Record<string, unknown>) => {
    if (!id) return;
    try {
      const json = await gql(
        `mutation updateIssue($id: ID!, $title: String, $description: String, $priority: Priority, $milestoneId: ID, $difficulty: Int, $startedAt: Time, $completedAt: Time, $environment: String, $branch: String, $link: String) {
          updateIssue(id: $id, title: $title, description: $description, priority: $priority, milestoneId: $milestoneId, difficulty: $difficulty, startedAt: $startedAt, completedAt: $completedAt, environment: $environment, branch: $branch, link: $link) {
            id title description priority milestone { id title }
          }
        }`,
        { id, ...fields }
      );
      if (!json.errors && json.data) {
        setIssue((prev) => prev ? { ...prev, ...json.data.updateIssue } : prev);
        toast.success("已更新");
      } else {
        toast.error(json.errors?.[0]?.message || "更新失败");
      }
    } catch {
      toast.error("网络错误，更新失败");
    }
  };

  const handleSaveTitle = async () => {
    if (!editTitle.trim()) return;
    await handleUpdateIssue({ title: editTitle.trim() });
    setEditingTitle(false);
  };

  const handleSaveDescription = async () => {
    await handleUpdateIssue({ description: editDescription || null });
    setEditingDescription(false);
  };

  const handleSavePriority = async (priority: string) => {
    await handleUpdateIssue({ priority });
    setEditPriority(false);
  };

  const handleSaveDifficulty = async (difficulty: number) => {
    await handleUpdateIssue({ difficulty });
    setEditDifficulty(false);
  };

  const handleSaveTimeFields = async () => {
    await handleUpdateIssue({
      startedAt: editStartedAt || null,
      completedAt: editCompletedAt || null,
    });
    setShowTimeFields(false);
  };

  const handleSaveEnv = async () => {
    await handleUpdateIssue({ environment: editEnv || null });
    setEditingEnv(false);
  };

  const handleSaveBranch = async () => {
    await handleUpdateIssue({ branch: editBranch || null });
    setEditingBranch(false);
  };


  const handleAddAssignee = async (agentId: string) => {
    if (!id) return;
    try {
      const json = await gql(
        `mutation addAssignee($issueID: ID!, $agentID: ID!) {
          addAssignee(issueID: $issueID, agentID: $agentID) { id agent { id name } state }
        }`,
        { issueID: id, agentID: agentId }
      );
      if (!json.errors) {
        setIssue((prev) => prev ? {
          ...prev,
          assignees: [...prev.assignees, json.data.addAssignee]
        } : prev);
        toast.success("已添加负责人");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，添加负责人失败");
    }
  };

  const handleRemoveAssignee = async (agentId: string, agentName?: string) => {
    if (!id) return;
    if (!window.confirm(`确认将「${agentName || agentId}」从负责人中移除？`)) return;
    try {
      const json = await gql(
        `mutation removeAssignee($issueID: ID!, $agentID: ID!) {
          removeAssignee(issueID: $issueID, agentID: $agentID)
        }`,
        { issueID: id, agentID: agentId }
      );
      if (!json.errors) {
        setIssue((prev) => prev ? {
          ...prev,
          assignees: prev.assignees.filter((a) => a.agent.id !== agentId)
        } : prev);
        toast.success("已移除负责人");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，移除负责人失败");
    }
  };

  const handleCreateMilestone = async () => {
    if (!newMilestoneTitle.trim() || !issue) return;
    try {
      const json = await gql(
        `mutation createMilestone($projectID: ID!, $title: String!) {
          createMilestone(projectID: $projectID, title: $title) { id title state }
        }`,
        { projectID: issue.projectID, title: newMilestoneTitle.trim() }
      );
      if (!json.errors) {
        setProjectMilestones((prev) => [...prev, json.data.createMilestone]);
        setNewMilestoneTitle("");
        setShowNewMilestone(false);
        toast.success("里程碑已创建");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，创建里程碑失败");
    }
  };

  const handleCreateLabel = async () => {
    if (!newLabelName.trim() || !issue) return;
    try {
      const json = await gql(
        `mutation createLabel($projectID: ID!, $name: String!, $color: String) {
          createLabel(projectID: $projectID, name: $name, color: $color) { id name color }
        }`,
        { projectID: issue.projectID, name: newLabelName.trim(), color: newLabelColor || null }
      );
      if (!json.errors) {
        const label = json.data.createLabel;
        setProjectLabels((prev) => [...prev, label]);
        setNewLabelName("");
        setShowNewLabel(false);
        // Auto-assign the newly created label to the issue
        await handleAddLabel(label.id);
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误，创建标签失败");
    }
  };

  // Fetch project agents for assignee picker
  useEffect(() => {
    if (!issue?.projectID) return;
    gql(
      `query agents($projectID: ID!) { agents(projectID: $projectID) { id name } }`,
      { projectID: issue.projectID }
    ).then((json) => {
      if (!json.errors) setProjectAgents(json.data.agents);
    });
  }, [issue?.projectID]);

  if (loading) return (
    <div className="space-y-4">
      <Skeleton className="h-6 w-48" />
      <Skeleton className="h-8 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );

  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;
  if (!issue) return <EmptyState title="Issue 不存在" />;

  const stateTransitions = transitions;
  const availableLabels = projectLabels.filter(
    (pl) => !(issue.labels || []).some((l) => l.id === pl.id)
  );
  const currentMilestoneId = issue.milestone?.id || "";

  const mainContent = (
    <div className="space-y-6">
      {/* Back link */}
      <Link to={`/projects/${issue.projectID}`} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        ← 返回项目
      </Link>

      {/* Header */}
      <div>
        <div className="flex items-center gap-2 flex-wrap">
          <Badge className={`text-xs ${stateBadgeColors[issue.state]}`}>
            {stateLabels[issue.state]}
          </Badge>
          <span className="text-xs text-muted-foreground">#{issue.number}</span>
          {editPriority && agent ? (
            <Select defaultValue={issue.priority} onValueChange={handleSavePriority} onOpenChange={(open) => { if (!open) setEditPriority(false); }}>
              <SelectTrigger className="h-5 w-14 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(priorityLabels).map(([v, l]) => (
                  <SelectItem key={v} value={v}>{l}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Badge variant="outline" className="text-xs cursor-pointer" onClick={() => agent && setEditPriority(true)}>
              {priorityLabels[issue.priority] || issue.priority}
            </Badge>
          )}
          {/* Difficulty */}
          {editDifficulty && agent ? (
            <Select defaultValue={String(issue.difficulty || 1)} onValueChange={(v) => handleSaveDifficulty(Number(v))} onOpenChange={(open) => { if (!open) setEditDifficulty(false); }}>
              <SelectTrigger className="h-5 w-16 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[1,2,3,4,5].map((v) => (
                  <SelectItem key={v} value={String(v)}>{'⭐'.repeat(v)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : issue.difficulty ? (
            <Badge variant="outline" className="text-xs cursor-pointer" onClick={() => agent && setEditDifficulty(true)} title="点击修改难度">
              {'⭐'.repeat(issue.difficulty)}
            </Badge>
          ) : agent ? (
            <Badge variant="outline" className="text-xs cursor-pointer text-muted-foreground" onClick={() => setEditDifficulty(true)}>
              +难度
            </Badge>
          ) : null}
          {/* Times */}
          {issue.startedAt && (
            <span className="text-xs text-muted-foreground">开始: {new Date(issue.startedAt).toLocaleString()}</span>
          )}
          {issue.completedAt && (
            <span className="text-xs text-muted-foreground">完成: {new Date(issue.completedAt).toLocaleString()}</span>
          )}
          {issue.dueDate && (
            <span className="text-xs text-muted-foreground">截止: {issue.dueDate.slice(0, 10)}</span>
          )}
        </div>
        {editingTitle ? (
          <div className="mt-2 flex gap-2">
            <Input
              value={editTitle}
              onChange={(e) => setEditTitle(e.target.value)}
              className="text-xl font-semibold"
              autoFocus
              onKeyDown={(e) => { if (e.key === "Enter") handleSaveTitle(); if (e.key === "Escape") setEditingTitle(false); }}
            />
            <Button size="sm" onClick={handleSaveTitle}><Check className="h-4 w-4" /></Button>
            <Button size="sm" variant="ghost" onClick={() => setEditingTitle(false)}><X className="h-4 w-4" /></Button>
          </div>
        ) : (
          <div className="mt-2 flex items-center gap-2">
            <h1 className="text-2xl font-semibold">{issue.title}</h1>
            {agent && (
              <Button variant="ghost" size="sm" className="h-6 px-2" onClick={() => { setEditTitle(issue.title); setEditingTitle(true); }}>
                <Pencil className="h-3 w-3" />
              </Button>
            )}
          </div>
        )}
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{issue.creator.name}</span>
          <span>创建于 {relativeTime(issue.createdAt)}</span>
        </div>
      </div>

      {/* Transitions */}
      <div className="flex flex-wrap gap-2 justify-between items-center">
        <div className="flex flex-wrap gap-2">
          {stateTransitions.length > 0 && agent && stateTransitions.map((state) => (
            <Button
              key={state}
              variant="outline"
              size="sm"
              onClick={() => handleTransition(state)}
            >
              转为 {stateLabels[state]}
            </Button>
          ))}
        </div>
        {agent && (
          <Button variant="destructive" size="sm" onClick={handleDelete}>
            <Trash2 className="h-4 w-4 mr-1" />
            删除
          </Button>
        )}
      </div>

      {/* Description */}
      {editingDescription ? (
        <Card>
          <CardContent className="p-4 space-y-2">
            <Textarea
              value={editDescription}
              onChange={(e) => setEditDescription(e.target.value)}
              rows={6}
              autoFocus
            />
            <div className="flex justify-end gap-2">
              <Button size="sm" onClick={handleSaveDescription}><Check className="h-4 w-4 mr-1" />保存</Button>
              <Button size="sm" variant="ghost" onClick={() => setEditingDescription(false)}>取消</Button>
            </div>
          </CardContent>
        </Card>
      ) : issue.description ? (
        <Card>
          <CardContent className="p-4 group relative">
            <MarkdownContent content={issue.description} />
            {agent && (
              <Button variant="ghost" size="sm" className="absolute top-2 right-2 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
                onClick={() => { setEditDescription(issue.description || ""); setEditingDescription(true); }}>
                <Pencil className="h-3 w-3" />
              </Button>
            )}
          </CardContent>
        </Card>
      ) : agent ? (
        <Card>
          <CardContent className="p-4">
            <Button variant="ghost" size="sm" onClick={() => { setEditDescription(""); setEditingDescription(true); }}>
              <Plus className="h-4 w-4 mr-1" />添加描述
            </Button>
          </CardContent>
        </Card>
      ) : null}

      <Separator />

      {/* Comments */}
      <div className="space-y-4">
        <h2 className="text-lg font-medium flex items-center gap-2">
          <MessageSquare className="h-4 w-4" />
          评论 ({comments.length})
        </h2>
        {comments
          .filter((c) => !c.parentID)
          .map((parent) => (
          <div key={parent.id}>
            <Card>
              <CardContent className="p-4">
                <div className="flex items-center gap-2">
                  <Avatar className="h-6 w-6">
                    <AvatarFallback className="text-xs">
                      {parent.author.name.charAt(0)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-sm font-medium">{parent.author.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {relativeTime(parent.createdAt)}
                  </span>
                  {agent && (
                    <Button variant="ghost" size="sm" className="h-6 px-2 text-xs ml-auto"
                      onClick={() => { setReplyingTo(replyingTo === parent.id ? null : parent.id); setReplyText(""); }}>
                      {replyingTo === parent.id ? "取消回复" : "回复"}
                    </Button>
                  )}
                </div>
                <div className="mt-2 text-sm">
                  <MarkdownContent content={parent.body} />
                </div>
                {replyingTo === parent.id && (
                  <div className="mt-2 border-t pt-2">
                    <div className="flex gap-2 mb-2">
                      <button
                        className={`text-xs font-medium px-2 py-0.5 rounded ${!previewReply ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
                        onClick={() => setPreviewReply(false)}
                      >
                        编辑
                      </button>
                      <button
                        className={`text-xs font-medium px-2 py-0.5 rounded ${previewReply ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
                        onClick={() => setPreviewReply(true)}
                      >
                        预览
                      </button>
                    </div>
                    {previewReply ? (
                      <div className="min-h-[60px] rounded-md border bg-background p-2 text-sm">
                        {replyText.trim() ? (
                          <MarkdownContent content={replyText} />
                        ) : (
                          <p className="text-xs text-muted-foreground">暂无内容</p>
                        )}
                      </div>
                    ) : (
                      <Textarea
                        value={replyText}
                        onChange={(e) => setReplyText(e.target.value)}
                        placeholder={`回复 ${parent.author.name}...`}
                        rows={2}
                        className="text-sm"
                      />
                    )}
                    <div className="mt-1 flex justify-end gap-1">
                      <Button size="sm" onClick={() => handleComment(parent.id)} disabled={!replyText.trim()}><Send className="h-3 w-3 mr-1" />发送</Button>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
            {/* Replies */}
            {parent.replies && parent.replies.length > 0 && (
              <div className="ml-6 mt-1 space-y-1">
                {parent.replies.map((r) => (
                  <Card key={r.id} className="border-muted">
                    <CardContent className="p-3">
                      <div className="flex items-center gap-2">
                        <Avatar className="h-5 w-5">
                          <AvatarFallback className="text-[10px]">
                            {r.author.name.charAt(0)}
                          </AvatarFallback>
                        </Avatar>
                        <span className="text-xs font-medium">{r.author.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {relativeTime(r.createdAt)}
                        </span>
                      </div>
                      <div className="mt-1 text-sm">
                        <MarkdownContent content={r.body} />
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </div>
        ))}

        {/* Add comment */}
        <Card>
          <CardContent className="p-4">
            <div className="flex gap-2 mb-2">
              <button
                className={`text-xs font-medium px-2 py-1 rounded ${!previewComment ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setPreviewComment(false)}
              >
                编辑
              </button>
              <button
                className={`text-xs font-medium px-2 py-1 rounded ${previewComment ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setPreviewComment(true)}
              >
                预览
              </button>
            </div>
            {previewComment ? (
              <div className="min-h-[80px] rounded-md border bg-background p-3">
                {newComment.trim() ? (
                  <MarkdownContent content={newComment} />
                ) : (
                  <p className="text-sm text-muted-foreground">暂无内容</p>
                )}
              </div>
            ) : (
              <Textarea
                value={newComment}
                onChange={(e) => setNewComment(e.target.value)}
                placeholder="输入评论... 支持 Markdown"
                rows={3}
              />
            )}
            <div className="mt-2 flex justify-end">
              <Button
                onClick={() => handleComment()}
                disabled={!newComment.trim()}
              >
                <Send className="mr-1 h-4 w-4" />
                发送
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <Separator />

      {/* Timeline */}
      <div className="space-y-2">
        <h2 className="text-lg font-medium">动态</h2>
        {events.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无动态</p>
        ) : (
          <Timeline events={events} />
        )}
      </div>
    </div>
  );

  const metaSidebar = (
    <div className="space-y-4">
      {/* Assignees */}
      <Card>
        <CardHeader className="pb-2 flex flex-row items-center justify-between">
          <CardTitle className="text-sm font-medium">负责人</CardTitle>
          {agent && (
            <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => setShowAssigneePicker(!showAssigneePicker)}>
              <Plus className="h-3 w-3 mr-1" />添加
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {(!issue.assignees || issue.assignees.length === 0) && !showAssigneePicker ? (
            <p className="text-sm text-muted-foreground">无</p>
          ) : (
            <div className="space-y-2">
              {issue.assignees.map((a) => (
                <div key={a.id} className="flex items-center gap-2 group">
                  <Avatar className="h-6 w-6">
                    <AvatarFallback className="text-xs">
                      {a.agent.name.charAt(0)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-sm">{a.agent.name}</span>
                  <Badge variant="outline" className="text-xs ml-auto">
                    {a.state === "accepted" ? "已接受" : a.state === "declined" ? "已拒绝" : "待处理"}
                  </Badge>
                  {agent && (
                    <button className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive transition-opacity"
                      onClick={() => handleRemoveAssignee(a.agent.id, a.agent.name)}>
                      <X className="h-3 w-3" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
          {showAssigneePicker && (
            <div className="mt-2 pt-2 border-t space-y-1">
              {projectAgents.length === 0 ? (
                <p className="text-xs text-muted-foreground">暂无可用成员</p>
              ) : (
                projectAgents
                  .filter((ag) => !(issue.assignees || []).some((a) => a.agent.id === ag.id))
                  .map((ag) => (
                    <div key={ag.id} className="flex items-center gap-2 py-1 cursor-pointer hover:bg-accent rounded px-1"
                      onClick={() => { handleAddAssignee(ag.id); setShowAssigneePicker(false); }}>
                      <Avatar className="h-5 w-5">
                        <AvatarFallback className="text-[10px]">{ag.name.charAt(0)}</AvatarFallback>
                      </Avatar>
                      <span className="text-sm">{ag.name}</span>
                    </div>
                  ))
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Milestone */}
      <Card>
        <CardHeader className="pb-2 flex flex-row items-center justify-between">
          <CardTitle className="text-sm font-medium">里程碑</CardTitle>
          {agent && (
            <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => setShowNewMilestone(!showNewMilestone)}>
              <Plus className="h-3 w-3 mr-1" />创建
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {showNewMilestone ? (
            <div className="space-y-2">
              <Input
                value={newMilestoneTitle}
                onChange={(e) => setNewMilestoneTitle(e.target.value)}
                placeholder="里程碑名称"
                className="h-8 text-sm"
                autoFocus
                onKeyDown={(e) => { if (e.key === "Enter") handleCreateMilestone(); if (e.key === "Escape") setShowNewMilestone(false); }}
              />
              <div className="flex justify-end gap-1">
                <Button size="sm" className="h-7 text-xs" onClick={handleCreateMilestone} disabled={!newMilestoneTitle.trim()}>
                  <Check className="h-3 w-3 mr-1" />创建
                </Button>
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => { setShowNewMilestone(false); setNewMilestoneTitle(""); }}>
                  取消
                </Button>
              </div>
            </div>
          ) : projectMilestones.length === 0 ? (
            <p className="text-sm text-muted-foreground">无</p>
          ) : (
            <Select value={currentMilestoneId || "_none"} onValueChange={(v) => handleChangeMilestone(v === "_none" ? "" : v)}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="不关联" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="_none">不关联</SelectItem>
                {projectMilestones.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </CardContent>
      </Card>

      {/* Labels */}
      <Card>
        <CardHeader className="pb-2 flex flex-row items-center justify-between">
          <CardTitle className="text-sm font-medium">标签</CardTitle>
          <div className="flex gap-1">
            {agent && (
              <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => setShowNewLabel(!showNewLabel)}>
                <Plus className="h-3 w-3 mr-1" />创建
              </Button>
            )}
            {availableLabels.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs"
                onClick={() => setShowLabelPicker(!showLabelPicker)}
              >
                <Plus className="h-3 w-3 mr-1" />
                添加
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {showNewLabel ? (
            <div className="space-y-2">
              <Input
                value={newLabelName}
                onChange={(e) => setNewLabelName(e.target.value)}
                placeholder="标签名称"
                className="h-8 text-sm"
                autoFocus
                onKeyDown={(e) => { if (e.key === "Enter") handleCreateLabel(); }}
              />
              <div className="flex items-center gap-2">
                <input
                  type="color"
                  value={newLabelColor}
                  onChange={(e) => setNewLabelColor(e.target.value)}
                  className="h-7 w-10 rounded border cursor-pointer"
                />
                <Button size="sm" className="h-7 text-xs" onClick={handleCreateLabel} disabled={!newLabelName.trim()}>
                  <Check className="h-3 w-3 mr-1" />创建
                </Button>
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => { setShowNewLabel(false); setNewLabelName(""); }}>
                  取消
                </Button>
                <Badge
                  className="text-xs ml-auto"
                  style={{
                    backgroundColor: `${newLabelColor}20`,
                    color: newLabelColor,
                  }}
                >
                  {newLabelName || "预览"}
                </Badge>
              </div>
            </div>
          ) : null}
          {(!issue.labels || issue.labels.length === 0) && !showLabelPicker ? (
            <p className="text-sm text-muted-foreground">无</p>
          ) : (
            <div className="flex flex-wrap gap-1">
              {issue.labels.map((l) => (
                <Badge
                  key={l.id}
                  className="text-xs cursor-pointer hover:opacity-80"
                  style={{
                    backgroundColor: l.color ? `${l.color}20` : undefined,
                    color: l.color || undefined,
                  }}
                  onClick={() => handleRemoveLabel(l.id, l.name)}
                  title="点击移除"
                >
                  {l.name} ✕
                </Badge>
              ))}
            </div>
          )}
          {showLabelPicker && availableLabels.length > 0 && (
            <div className="mt-2 pt-2 border-t flex flex-wrap gap-1">
              {availableLabels.map((l) => (
                <Badge
                  key={l.id}
                  variant="outline"
                  className="text-xs cursor-pointer hover:bg-accent"
                  style={{
                    borderColor: l.color || undefined,
                    color: l.color || undefined,
                  }}
                  onClick={() => {
                    handleAddLabel(l.id);
                    setShowLabelPicker(false);
                  }}
                >
                  + {l.name}
                </Badge>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Environment / Branch / Link */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">环境 / 分支 / 链接</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {showTimeFields ? (
            <div className="space-y-2 border-b pb-2 mb-2">
              <div>
                <span className="text-xs text-muted-foreground">开始时间</span>
                <Input
                  type="datetime-local"
                  value={editStartedAt ? new Date(editStartedAt).toISOString().slice(0, 16) : ""}
                  onChange={(e) => setEditStartedAt(e.target.value ? new Date(e.target.value).toISOString() : "")}
                  className="h-8 text-sm mt-1"
                />
              </div>
              <div>
                <span className="text-xs text-muted-foreground">完成时间</span>
                <Input
                  type="datetime-local"
                  value={editCompletedAt ? new Date(editCompletedAt).toISOString().slice(0, 16) : ""}
                  onChange={(e) => setEditCompletedAt(e.target.value ? new Date(e.target.value).toISOString() : "")}
                  className="h-8 text-sm mt-1"
                />
              </div>
              <div className="flex justify-end gap-1">
                <Button size="sm" className="h-7 text-xs" onClick={handleSaveTimeFields}>保存</Button>
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setShowTimeFields(false)}>取消</Button>
              </div>
            </div>
          ) : agent ? (
            <button className="text-xs text-primary hover:underline w-full text-left" onClick={() => {
              setEditStartedAt(issue.startedAt || "");
              setEditCompletedAt(issue.completedAt || "");
              setShowTimeFields(true);
            }}>
              编辑时间
            </button>
          ) : null}
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground w-12">环境</span>
              {editingEnv ? (
                <div className="flex-1 flex gap-1">
                  <Input value={editEnv} onChange={(e) => setEditEnv(e.target.value)} className="h-7 text-xs flex-1"
                    autoFocus onKeyDown={(e) => { if (e.key === "Enter") handleSaveEnv(); if (e.key === "Escape") setEditingEnv(false); }} />
                  <Button size="sm" className="h-7 text-xs" onClick={handleSaveEnv}>确定</Button>
                </div>
              ) : (
                <span className="text-xs flex-1 truncate cursor-pointer hover:text-primary"
                  onClick={() => { setEditEnv(issue.environment || ""); setEditingEnv(true); }}>
                  {issue.environment || <span className="text-muted-foreground">未设置</span>}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground w-12">分支</span>
              {editingBranch ? (
                <div className="flex-1 flex gap-1">
                  <Input value={editBranch} onChange={(e) => setEditBranch(e.target.value)} className="h-7 text-xs flex-1"
                    autoFocus onKeyDown={(e) => { if (e.key === "Enter") handleSaveBranch(); if (e.key === "Escape") setEditingBranch(false); }} />
                  <Button size="sm" className="h-7 text-xs" onClick={handleSaveBranch}>确定</Button>
                </div>
              ) : (
                <span className="text-xs flex-1 truncate cursor-pointer hover:text-primary"
                  onClick={() => { setEditBranch(issue.branch || ""); setEditingBranch(true); }}>
                  {issue.branch || <span className="text-muted-foreground">未设置</span>}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground w-12">链接</span>
              <span className="text-xs flex-1 truncate">
                {issue.link ? <a href={issue.link} target="_blank" rel="noreferrer" className="text-primary underline">打开链接</a> : <span className="text-muted-foreground">未设置</span>}
              </span>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );

  if (isDesktop) {
    return (
      <div className="flex gap-6">
        <div className="flex-1 min-w-0">{mainContent}</div>
        <div className="w-80 shrink-0">{metaSidebar}</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {mainContent}
      <Separator />
      {metaSidebar}
    </div>
  );
}
