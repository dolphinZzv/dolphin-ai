import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import Markdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";

import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Send, MessageSquare, Trash2, Plus } from "lucide-react";
import { toast } from "sonner";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";

interface IssueDetail {
  id: string;
  number: number;
  title: string;
  description: string | null;
  state: string;
  priority: string;
  dueDate: string | null;
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
  closed_completed: "已完成",
  closed_not_planned: "已关闭",
};

const stateBadgeColors: Record<string, string> = {
  open: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  in_progress: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  blocked: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  review: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200",
  closed_completed: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200",
  closed_not_planned: "bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400",
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

const validTransitions: Record<string, string[]> = {
  open: ["in_progress", "blocked"],
  in_progress: ["review", "blocked"],
  blocked: ["in_progress", "open"],
  review: ["closed_completed", "closed_not_planned", "in_progress"],
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

function MarkdownContent({ content }: { content: string }) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none">
      <Markdown rehypePlugins={[rehypeSanitize]}>{content}</Markdown>
    </div>
  );
}

function Timeline({ events }: { events: TimelineEvent[] }) {
  return (
    <div className="space-y-0">
      {events.map((event, idx) => (
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
          </div>
        </div>
      ))}
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

  const gql = useCallback(
    (operationName: string, query: string, variables: Record<string, unknown>) => {
      const token = typeof window !== "undefined" ? localStorage.getItem("token") : null;
      const headers: Record<string, string> = { "Content-Type": "application/json" };
      if (token) headers["Authorization"] = `Bearer ${token}`;
      return fetch("/graphql", {
        method: "POST",
        headers,
        body: JSON.stringify({ operationName, query, variables }),
      }).then((r) => r.json());
    },
    []
  );

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    Promise.all([
      gql(
        "issue",
        `query issue($id: ID!) {
          issue(id: $id) {
            id number title description state priority dueDate createdAt
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
        "comments",
        `query comments($issueId: ID!) {
          comments(issueID: $issueId) { id body createdAt author { id name } }
        }`,
        { issueId: id }
      ),
      gql(
        "timeline",
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
        setComments(cJson.data.comments);
        setEvents(tJson.data.timeline);

        // Fetch project labels and milestones
        const pid = iJson.data.issue.projectID;
        if (pid) {
          gql(
            "projectLabels",
            `query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`,
            { projectId: pid }
          ).then((lJson) => {
            if (!lJson.errors) setProjectLabels(lJson.data.labels);
          });
          gql(
            "projectMilestones",
            `query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title state } }`,
            { projectId: pid }
          ).then((mJson) => {
            if (!mJson.errors) setProjectMilestones(mJson.data.milestones);
          });
        }
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id, gql]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleTransition = async (toState: string) => {
    if (!id || !agent) return;
    const json = await gql(
      "transitionIssue",
      `mutation transitionIssue($id: ID!, $newState: IssueState!, $actorId: ID!) {
        transitionIssue(id: $id, newState: $newState, actorID: $actorId) { state }
      }`,
      { id, newState: toState, actorId: agent.agentId }
    );
    if (!json.errors && json.data) {
      setIssue((prev) => prev ? { ...prev, state: json.data.transitionIssue.state } : prev);
      toast.success(`状态已变更为 ${stateLabels[toState]}`);
      fetchData();
    } else {
      toast.error(json.errors?.[0]?.message || "状态变更失败");
    }
  };

  const handleDelete = async () => {
    if (!id || !window.confirm("确定删除这个 Issue？此操作不可撤销。")) return;
    const json = await gql(
      "deleteIssue",
      `mutation deleteIssue($id: ID!) { deleteIssue(id: $id) }`,
      { id }
    );
    if (!json.errors) {
      toast.success("Issue 已删除");
      if (issue?.projectID) window.location.href = `/projects/${issue.projectID}`;
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleComment = async () => {
    if (!newComment.trim() || !id || !agent) return;
    const json = await gql(
      "addComment",
      `mutation addComment($issueID: ID!, $authorID: ID!, $body: String!, $contentType: CommentContentType!) {
        addComment(issueID: $issueID, authorID: $authorID, body: $body, contentType: $contentType) { id body createdAt author { id name } }
      }`,
      {
        issueID: id,
        authorID: agent.agentId,
        body: newComment,
        contentType: "markdown",
      }
    );
    if (json.data) {
      setComments((prev) => [...prev, json.data.addComment]);
      setNewComment("");
      toast.success("评论发送成功");
      fetchData();
    }
  };

  const handleAddLabel = async (labelId: string) => {
    if (!id) return;
    const json = await gql(
      "addLabels",
      `mutation addLabels($issueID: ID!, $labelIDs: [ID!]!) { addLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
      { issueID: id, labelIDs: [labelId] }
    );
    if (!json.errors) {
      setIssue((prev) => prev ? { ...prev, labels: json.data.addLabels.labels } : prev);
      toast.success("标签已添加");
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleRemoveLabel = async (labelId: string) => {
    if (!id) return;
    const json = await gql(
      "removeLabels",
      `mutation removeLabels($issueID: ID!, $labelIDs: [ID!]!) { removeLabels(issueID: $issueID, labelIDs: $labelIDs) { id labels { id name color } } }`,
      { issueID: id, labelIDs: [labelId] }
    );
    if (!json.errors) {
      setIssue((prev) => prev ? { ...prev, labels: json.data.removeLabels.labels } : prev);
      toast.success("标签已移除");
    } else {
      toast.error(json.errors[0].message);
    }
  };

  const handleChangeMilestone = async (milestoneId: string) => {
    if (!id) return;
    const json = await gql(
      "updateIssue",
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
  };

  if (loading) return (
    <div className="space-y-4">
      <Skeleton className="h-6 w-48" />
      <Skeleton className="h-8 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );

  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;
  if (!issue) return <EmptyState title="Issue 不存在" />;

  const transitions = validTransitions[issue.state] || [];
  const availableLabels = projectLabels.filter(
    (pl) => !issue.labels.some((l) => l.id === pl.id)
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
          <Badge variant="outline" className="text-xs">
            {priorityLabels[issue.priority] || issue.priority}
          </Badge>
          {issue.dueDate && (
            <span className="text-xs text-muted-foreground">
              截止: {issue.dueDate.slice(0, 10)}
            </span>
          )}
        </div>
        <h1 className="mt-2 text-2xl font-semibold">{issue.title}</h1>
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{issue.creator.name}</span>
          <span>创建于 {relativeTime(issue.createdAt)}</span>
        </div>
      </div>

      {/* Transitions */}
      <div className="flex flex-wrap gap-2 justify-between items-center">
        <div className="flex flex-wrap gap-2">
          {transitions.length > 0 && agent && transitions.map((state) => (
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
      {issue.description && (
        <Card>
          <CardContent className="p-4">
            <MarkdownContent content={issue.description} />
          </CardContent>
        </Card>
      )}

      <Separator />

      {/* Comments */}
      <div className="space-y-4">
        <h2 className="text-lg font-medium flex items-center gap-2">
          <MessageSquare className="h-4 w-4" />
          评论 ({comments.length})
        </h2>
        {comments.map((c) => (
          <Card key={c.id}>
            <CardContent className="p-4">
              <div className="flex items-center gap-2">
                <Avatar className="h-6 w-6">
                  <AvatarFallback className="text-xs">
                    {c.author.name.charAt(0)}
                  </AvatarFallback>
                </Avatar>
                <span className="text-sm font-medium">{c.author.name}</span>
                <span className="text-xs text-muted-foreground">
                  {relativeTime(c.createdAt)}
                </span>
              </div>
              <div className="mt-2 text-sm">
                <MarkdownContent content={c.body} />
              </div>
            </CardContent>
          </Card>
        ))}

        {/* Add comment */}
        <Card>
          <CardContent className="p-4">
            <Textarea
              value={newComment}
              onChange={(e) => setNewComment(e.target.value)}
              placeholder="输入评论... 支持 Markdown"
              rows={3}
            />
            <div className="mt-2 flex justify-end">
              <Button
                onClick={handleComment}
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
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">负责人</CardTitle>
        </CardHeader>
        <CardContent>
          {!issue.assignees || issue.assignees.length === 0 ? (
            <p className="text-sm text-muted-foreground">无</p>
          ) : (
            <div className="space-y-2">
              {issue.assignees.map((a) => (
                <div key={a.id} className="flex items-center gap-2">
                  <Avatar className="h-6 w-6">
                    <AvatarFallback className="text-xs">
                      {a.agent.name.charAt(0)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-sm">{a.agent.name}</span>
                  <Badge variant="outline" className="text-xs ml-auto">
                    {a.state === "accepted" ? "已接受" : a.state === "declined" ? "已拒绝" : "待处理"}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Milestone */}
      <Card>
        <CardHeader className="pb-2 flex flex-row items-center justify-between">
          <CardTitle className="text-sm font-medium">里程碑</CardTitle>
        </CardHeader>
        <CardContent>
          {projectMilestones.length === 0 ? (
            <p className="text-sm text-muted-foreground">无</p>
          ) : (
            <Select value={currentMilestoneId} onValueChange={handleChangeMilestone}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="不关联" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">不关联</SelectItem>
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
        </CardHeader>
        <CardContent>
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
                  onClick={() => handleRemoveLabel(l.id)}
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
