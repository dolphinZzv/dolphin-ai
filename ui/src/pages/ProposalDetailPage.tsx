import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { gql } from "@/lib/graphql";
import Markdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Separator } from "@/components/ui/separator";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { MessageSquare, Send, Plus } from "lucide-react";
import { toast } from "sonner";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { CreateTaskDialog } from "@/components/project/CreateTaskDialog";

interface Agent {
  id: string;
  name: string;
}

interface TaskItem {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
  assignee: Agent | null;
}

interface ProposalDetail {
  id: string;
  number: number;
  title: string;
  description: string | null;
  state: string;
  priority: string;
  author: Agent;
  reviewer: Agent | null;
  reviewNote: string | null;
  reviewedAt: string | null;
  submittedAt: string | null;
  approvedAt: string | null;
  startedAt: string | null;
  completedAt: string | null;
  cancelledAt: string | null;
  createdAt: string;
  tasks: TaskItem[];
  labels: Array<{ id: string; name: string; color: string | null }>;
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

const stateLabels: Record<string, string> = {
  draft: "草稿",
  submitted: "已提交",
  under_review: "评审中",
  approved: "已通过",
  rejected: "已驳回",
  in_execution: "执行中",
  completed: "已完成",
  cancelled: "已取消",
};

const stateBadgeColors: Record<string, string> = {
  draft: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200",
  submitted: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  under_review: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200",
  approved: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  rejected: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  in_execution: "bg-cyan-100 text-cyan-800 dark:bg-cyan-900 dark:text-cyan-200",
  completed: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200",
  cancelled: "bg-slate-100 text-slate-500 dark:bg-slate-900 dark:text-slate-400",
};

const priorityLabels: Record<string, string> = {
  critical: "关键", high: "高", medium: "中", low: "低",
};

const eventTypeLabels: Record<string, string> = {
  proposal_created: "创建了提案",
  proposal_state_changed: "变更了状态",
};

const taskStateLabels: Record<string, string> = {
  pending: "待处理", in_progress: "进行中", completed: "已完成", blocked: "阻塞", cancelled: "已取消",
};

function relativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const diffMs = Date.now() - date.getTime();
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
      <Markdown rehypePlugins={[rehypeSanitize]} remarkPlugins={[remarkGfm]}>{content}</Markdown>
    </div>
  );
}

export function ProposalDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { agent } = useAuth();

  const [proposal, setProposal] = useState<ProposalDetail | null>(null);
  const [comments, setComments] = useState<Comment[]>([]);
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newComment, setNewComment] = useState("");
  const [transitions, setTransitions] = useState<string[]>([]);
  const [showTaskDialog, setShowTaskDialog] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    Promise.all([
      gql(
        `query proposal($id: ID!) {
          proposal(id: $id) {
            id number title description state priority
            author { id name }
            reviewer { id name }
            reviewNote reviewedAt submittedAt approvedAt startedAt completedAt cancelledAt createdAt
            tasks { id number title state priority assignee { id name } }
            labels { id name color }
          }
        }`,
        { id }
      ),
      gql(
        `query comments($proposalId: ID!) {
          comments(proposalID: $proposalId) { id body createdAt parentID author { id name } replies { id body createdAt author { id name } } }
        }`,
        { proposalId: id }
      ),
      gql(
        `query timeline($proposalId: ID!) {
          timeline(proposalID: $proposalId) { id eventType createdAt actor { id name } payload }
        }`,
        { proposalId: id }
      ),
    ])
      .then(([pJson, cJson, tJson]) => {
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (cJson.errors) { setError(cJson.errors[0].message); return; }
        if (tJson.errors) { setError(tJson.errors[0].message); return; }
        setProposal(pJson.data.proposal);
        setComments((cJson.data.comments || []).map((c: Comment) => ({
          ...c,
          replies: [...(c.replies || [])].sort(
            (a: Comment, b: Comment) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
          ),
        })).sort(
          (a: Comment, b: Comment) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
        ));
        setEvents(tJson.data.timeline);

        const state = pJson.data.proposal.state;
        gql(`query vt($state: ProposalState!) { validProposalTransitions(state: $state) }`, { state })
          .then((vJson) => { if (!vJson.errors) setTransitions(vJson.data.validProposalTransitions); });
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleTransition = async (toState: string) => {
    if (!id || !agent) return;
    const label = stateLabels[toState] || toState;
    const note = window.prompt(`请输入变更为「${label}」的备注说明（可选）：`);
    if (note === null) return;
    try {
      const json = await gql(
        `mutation transitionProposal($id: ID!, $newState: ProposalState!, $actorID: ID!, $note: String) {
          transitionProposal(id: $id, newState: $newState, actorID: $actorID, note: $note) { state }
        }`,
        { id, newState: toState, actorID: agent.agentId, note: note || null }
      );
      if (!json.errors) {
        setProposal((prev) => prev ? { ...prev, state: json.data.transitionProposal.state } : prev);
        toast.success(`状态已变更为 ${label}`);
        fetchData();
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误");
    }
  };

  const handleComment = async () => {
    if (!newComment.trim() || !id || !agent) return;
    try {
      const json = await gql(
        `mutation addProposalComment($proposalID: ID!, $authorID: ID!, $body: String!, $contentType: CommentContentType!) {
          addProposalComment(proposalID: $proposalID, authorID: $authorID, body: $body, contentType: $contentType) { id body }
        }`,
        { proposalID: id, authorID: agent.agentId, body: newComment, contentType: "markdown" }
      );
      if (!json.errors) {
        setNewComment("");
        fetchData();
        toast.success("评论发送成功");
      } else {
        toast.error(json.errors[0].message);
      }
    } catch {
      toast.error("网络错误");
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
  if (!proposal) return <EmptyState title="提案不存在" />;

  return (
    <div className="space-y-6 max-w-4xl">
      {/* Back */}
      <Link to={-1 as any} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        ← 返回
      </Link>

      {/* Header */}
      <div>
        <div className="flex items-center gap-2 flex-wrap">
          <Badge className={`text-xs ${stateBadgeColors[proposal.state]}`}>
            {stateLabels[proposal.state]}
          </Badge>
          <Badge variant="outline" className="text-xs">
            {priorityLabels[proposal.priority] || proposal.priority}
          </Badge>
          <span className="text-xs text-muted-foreground">#{proposal.number}</span>
        </div>
        <h1 className="text-2xl font-semibold mt-2">{proposal.title}</h1>
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{proposal.author.name}</span>
          <span>创建于 {relativeTime(proposal.createdAt)}</span>
        </div>
      </div>

      {/* Transitions */}
      <div className="flex flex-wrap gap-2">
        {transitions.length > 0 && agent && transitions.map((s) => (
          <Button key={s} variant="outline" size="sm" onClick={() => handleTransition(s)}>
            转为 {stateLabels[s]}
          </Button>
        ))}
        {(proposal.state === "approved" || proposal.state === "in_execution") && (
          <Button variant="outline" size="sm" onClick={() => setShowTaskDialog(true)}>
            <Plus className="h-4 w-4 mr-1" />创建任务
          </Button>
        )}
      </div>

      {/* Review info */}
      {proposal.reviewer && (
        <Card>
          <CardContent className="p-4 space-y-1">
            <div className="text-sm">
              评审人: <span className="font-medium">{proposal.reviewer.name}</span>
            </div>
            {proposal.reviewNote && (
              <div className="text-sm"><MarkdownContent content={proposal.reviewNote} /></div>
            )}
            {proposal.reviewedAt && (
              <div className="text-xs text-muted-foreground">{relativeTime(proposal.reviewedAt)}</div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Description */}
      {proposal.description && (
        <Card>
          <CardContent className="p-4">
            <MarkdownContent content={proposal.description} />
          </CardContent>
        </Card>
      )}

      {/* Tasks */}
      {proposal.tasks && proposal.tasks.length > 0 && (
        <div>
          <h2 className="text-lg font-medium mb-2">任务 ({proposal.tasks.length})</h2>
          <div className="space-y-2">
            {proposal.tasks.map((t) => (
              <Link key={t.id} to={`/tasks/${t.id}`}>
                <Card className="hover:bg-accent/50 transition-colors">
                  <CardContent className="p-3 flex items-center gap-3">
                    <Badge variant="secondary" className="text-xs shrink-0">{taskStateLabels[t.state]}</Badge>
                    <span className="text-sm font-medium">#{t.number} {t.title}</span>
                    {t.assignee && (
                      <span className="text-xs text-muted-foreground ml-auto">{t.assignee.name}</span>
                    )}
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
        </div>
      )}

      {/* Labels */}
      {proposal.labels && proposal.labels.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {proposal.labels.map((l) => (
            <Badge key={l.id} className="text-xs" style={{ backgroundColor: l.color ? `${l.color}20` : undefined, color: l.color || undefined }}>
              {l.name}
            </Badge>
          ))}
        </div>
      )}

      <Separator />

      {/* Comments */}
      <div className="space-y-4">
        <h2 className="text-lg font-medium flex items-center gap-2">
          <MessageSquare className="h-4 w-4" />
          评论 ({comments.length})
        </h2>
        {comments.filter((c) => !c.parentID).map((parent) => (
          <Card key={parent.id}>
            <CardContent className="p-4">
              <div className="flex items-center gap-2">
                <Avatar className="h-6 w-6">
                  <AvatarFallback className="text-xs">{parent.author.name.charAt(0)}</AvatarFallback>
                </Avatar>
                <span className="text-sm font-medium">{parent.author.name}</span>
                <span className="text-xs text-muted-foreground">{relativeTime(parent.createdAt)}</span>
              </div>
              <div className="mt-2 text-sm"><MarkdownContent content={parent.body} /></div>
              {parent.replies?.map((r) => (
                <div key={r.id} className="ml-6 mt-2 border-l-2 pl-3">
                  <div className="flex items-center gap-2">
                    <Avatar className="h-5 w-5">
                      <AvatarFallback className="text-[10px]">{r.author.name.charAt(0)}</AvatarFallback>
                    </Avatar>
                    <span className="text-xs font-medium">{r.author.name}</span>
                    <span className="text-xs text-muted-foreground">{relativeTime(r.createdAt)}</span>
                  </div>
                  <div className="mt-1 text-sm"><MarkdownContent content={r.body} /></div>
                </div>
              ))}
            </CardContent>
          </Card>
        ))}
        {agent && (
          <Card>
            <CardContent className="p-4">
              <Textarea value={newComment} onChange={(e) => setNewComment(e.target.value)} placeholder="输入评论... 支持 Markdown" rows={3} />
              <div className="mt-2 flex justify-end">
                <Button onClick={handleComment} disabled={!newComment.trim()}>
                  <Send className="mr-1 h-4 w-4" />发送
                </Button>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      <Separator />

      {/* Timeline */}
      <div className="space-y-2">
        <h2 className="text-lg font-medium">动态</h2>
        {events.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无动态</p>
        ) : (
          <div className="space-y-0">
            {events.map((ev, idx) => (
              <div key={ev.id} className="flex gap-3">
                <div className="flex flex-col items-center">
                  <div className="mt-1.5 flex h-5 w-5 items-center justify-center rounded-full border bg-card text-[10px] text-muted-foreground">●</div>
                  {idx < events.length - 1 && <div className="w-px flex-1 bg-border" />}
                </div>
                <div className="flex-1 pb-4">
                  <div className="flex items-baseline gap-2">
                    <span className="text-sm font-medium">{ev.actor.name}</span>
                    <span className="text-xs text-muted-foreground">{relativeTime(ev.createdAt)}</span>
                  </div>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {eventTypeLabels[ev.eventType] || ev.eventType}
                    {ev.payload?.note ? `: ${ev.payload.note}` : ""}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Create Task Dialog */}
      <CreateTaskDialog
        proposalId={id!}
        open={showTaskDialog}
        onOpenChange={setShowTaskDialog}
        onCreated={fetchData}
      />
    </div>
  );
}
