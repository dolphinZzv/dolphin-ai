import { useEffect, useState, useCallback } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { gql } from "@/lib/graphql";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { Plus, Trash2, AlertTriangle, Copy, Check } from "lucide-react";
import { toast } from "sonner";

interface Label {
  id: string;
  name: string;
  color: string | null;
}

interface Milestone {
  id: string;
  title: string;
  description: string | null;
  state: string;
  dueDate: string | null;
}

interface MemberAgent {
  id: string;
  number: number;
  name: string;
  kind: string;
  status: string;
  capabilities: string[];
  deviceInfo?: string;
  modelInfo?: string;
  lastIP?: string;
  externalID: string;
}

interface Member {
  agent: MemberAgent;
  role: string;
}

interface AgentBrief {
  id: string;
  name: string;
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

const PRESET_COLORS = ["#0366d6", "#28a745", "#d73a49", "#ffd33d", "#6f42c1", "#e4e669", "#f97583", "#888"];

export function ProjectSettingsPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<"labels" | "milestones" | "members" | "agents">("agents");
  const [labels, setLabels] = useState<Label[]>([]);
  const [milestones, setMilestones] = useState<Milestone[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // add member dialog
  const [addOpen, setAddOpen] = useState(false);
  const [allAgents, setAllAgents] = useState<AgentBrief[]>([]);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [selectedRole, setSelectedRole] = useState("member");
  const [adding, setAdding] = useState(false);

  // create label
  const [labelOpen, setLabelOpen] = useState(false);
  const [newLabelName, setNewLabelName] = useState("");
  const [newLabelColor, setNewLabelColor] = useState("#0366d6");
  const [labelCreating, setLabelCreating] = useState(false);

  // create milestone
  const [msOpen, setMsOpen] = useState(false);
  const [newMsTitle, setNewMsTitle] = useState("");
  const [newMsDesc, setNewMsDesc] = useState("");
  const [newMsDue, setNewMsDue] = useState("");
  const [msCreating, setMsCreating] = useState(false);

  // delete project
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // create agent
  const [agentOpen, setAgentOpen] = useState(false);
  const [newAgentName, setNewAgentName] = useState("");
  const [newAgentKind, setNewAgentKind] = useState("ai");
  const [newAgentModel, setNewAgentModel] = useState("");
  const [newAgentDevice, setNewAgentDevice] = useState("");
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [tokenCopied, setTokenCopied] = useState(false);
  const [cmdCopied, setCmdCopied] = useState(false);
  const [installTool, setInstallTool] = useState<"claude" | "opencode">("claude");
  const [installTab, setInstallTab] = useState<"cli" | "project" | "manual">("cli");
  const [agentCreating, setAgentCreating] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    Promise.all([
      gql(
        `query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`,
        { projectId: id }
      ),
      gql(
        `query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title description state dueDate } }`,
        { projectId: id }
      ),
      gql(
        `query project($id: ID!) {
          project(id: $id) {
members { agent { id number name kind status capabilities deviceInfo modelInfo lastIP externalID } role }
          }
        }`,
        { id }
      ),
      gql("query agents { agents { id name } }"),
    ])
      .then(([lJson, mJson, pJson, aJson]) => {
        if (lJson.errors) { setError(lJson.errors[0].message); return; }
        if (mJson.errors) { setError(mJson.errors[0].message); return; }
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (aJson.errors) { setError(aJson.errors[0].message); return; }
        setLabels(lJson.data.labels);
        setMilestones(mJson.data.milestones);
        setMembers(pJson.data.project.members || []);
        setAllAgents(aJson.data.agents || []);
      })
      .catch(() => setError("网络错误"))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleAddMember = async () => {
    if (!id || !selectedAgentId) return;
    setAdding(true);
    try {
      const json = await gql(
        `mutation addProjectMember($pid: ID!, $aid: ID!, $role: ProjectRole!) {
          addProjectMember(projectID: $pid, agentID: $aid, role: $role) { agent { id name } role }
        }`,
        { pid: id, aid: selectedAgentId, role: selectedRole }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("成员已添加");
      setAddOpen(false);
      setSelectedAgentId("");
      setSelectedRole("member");
      fetchData();
    } catch {
      toast.error("网络错误");
    } finally {
      setAdding(false);
    }
  };

  const handleRemoveMember = async (agentId: string) => {
    if (!id) return;
    try {
      const json = await gql(
        `mutation removeProjectMember($pid: ID!, $aid: ID!) {
          removeProjectMember(projectID: $pid, agentID: $aid)
        }`,
        { pid: id, aid: agentId }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("成员已移除");
      fetchData();
    } catch {
      toast.error("网络错误");
    }
  };

  const handleCreateLabel = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!id || !newLabelName.trim()) return;
    setLabelCreating(true);
    try {
      const json = await gql(
        `mutation createLabel($pid: ID!, $name: String!, $color: String) {
          createLabel(projectID: $pid, name: $name, color: $color) { id name color }
        }`,
        { pid: id, name: newLabelName, color: newLabelColor || null }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("标签已创建");
      setLabelOpen(false);
      setNewLabelName("");
      setNewLabelColor("#0366d6");
      fetchData();
    } catch {
      toast.error("网络错误");
    } finally {
      setLabelCreating(false);
    }
  };

  const handleDeleteLabel = async (labelId: string) => {
    try {
      const json = await gql(
        `mutation deleteLabel($id: ID!) { deleteLabel(id: $id) }`,
        { id: labelId }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("标签已删除");
      fetchData();
    } catch {
      toast.error("网络错误");
    }
  };

  const handleCreateMilestone = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!id || !newMsTitle.trim()) return;
    setMsCreating(true);
    try {
      const json = await gql(
        `mutation createMilestone($pid: ID!, $title: String!, $desc: String, $due: Time) {
          createMilestone(projectID: $pid, title: $title, description: $desc, dueDate: $due) { id title state }
        }`,
        {
          pid: id,
          title: newMsTitle,
          desc: newMsDesc || null,
          due: newMsDue ? new Date(newMsDue).toISOString() : null,
        }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("里程碑已创建");
      setMsOpen(false);
      setNewMsTitle("");
      setNewMsDesc("");
      setNewMsDue("");
      fetchData();
    } catch {
      toast.error("网络错误");
    } finally {
      setMsCreating(false);
    }
  };

  const handleDeleteMilestone = async (msId: string) => {
    try {
      const json = await gql(
        `mutation deleteMilestone($id: ID!) { deleteMilestone(id: $id) }`,
        { id: msId }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("里程碑已删除");
      fetchData();
    } catch {
      toast.error("网络错误");
    }
  };

  const handleDeleteProject = async () => {
    if (!id) return;
    setDeleting(true);
    try {
      const json = await gql(
        `mutation deleteProject($id: ID!) { deleteProject(id: $id) }`,
        { id }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }
      toast.success("项目已删除");
      navigate("/projects", { replace: true });
    } catch {
      toast.error("网络错误");
    } finally {
      setDeleting(false);
    }
  };

  const handleCreateAgent = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!id || !newAgentName.trim()) return;
    setAgentCreating(true);
    try {
            const json = await gql(
        `mutation createProjectAgent($pid: ID!, $name: String!, $kind: AgentKind!, $device: String, $model: String) {
          createProjectAgent(projectID: $pid, name: $name, kind: $kind, deviceInfo: $device, modelInfo: $model) { agent { id name } token }
        }`,
        {
          pid: id,
          name: newAgentName,
          kind: newAgentKind,
          device: newAgentDevice || null,
          model: newAgentModel || null,
        }
      );
      if (json.errors) { toast.error(json.errors[0].message); return; }

      const token = json.data?.createProjectAgent?.token;
      if (token) setCreatedToken(token);
      setAgentOpen(false);
      setNewAgentName("");
      setNewAgentKind("ai");
      setNewAgentModel("");
      setNewAgentDevice("");
      fetchData();
    } catch {
      toast.error("网络错误");
    } finally {
      setAgentCreating(false);
    }
  };

  const tabs = [
    { key: "agents" as const, label: "Agent" },
    { key: "labels" as const, label: "标签" },
    { key: "milestones" as const, label: "里程碑" },
    { key: "members" as const, label: "成员" },
  ];

  if (loading) return <Skeleton className="h-48 w-full" />;
  if (error) return <ErrorFallback message={error} onRetry={fetchData} />;

  const nonMemberAgents = allAgents.filter(
    (a) => !members.some((m) => m.agent.id === a.id)
  );

  return (
    <div className="space-y-4">
      <Link to={`/projects/${id}`} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        ← 返回项目
      </Link>
      <h1 className="text-2xl font-semibold">项目设置</h1>

      {/* Tabs */}
      <div className="flex gap-2 border-b overflow-x-auto">
        {tabs.map((t) => (
          <button
            key={t.key}
            className={`whitespace-nowrap px-3 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t.key
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Agents tab */}
      {tab === "agents" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">{members.length} 个 Agent</span>
            <Button size="sm" onClick={() => setAgentOpen(true)}>
              <Plus className="mr-1 h-4 w-4" />新建 Agent
            </Button>
          </div>

          {members.length === 0 ? (
            <EmptyState title="暂无 Agent" description="创建 Agent 并加入项目" />
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {members.map((m) => {
                const a = m.agent;
                const st = statusConfig[a.status] || statusConfig.offline;
                return (
                  <Link key={a.id} to={`/agents/${a.id}`} className="block">
                    <Card>
                      <CardContent className="p-4">
                        <div className="flex items-center gap-3">
                          <div className={`h-3 w-3 rounded-full ${st.dot}`} />
                          <div className="flex-1 min-w-0">
                            <p className="font-medium truncate">#{a.number} {a.name}</p>
                            <p className="text-xs text-muted-foreground">
                              {kindLabels[a.kind] || a.kind} · {st.label}
                            </p>
                          </div>
                          <Badge variant="secondary" className="text-xs">
                            {m.role === "owner" ? "拥有者" : m.role === "member" ? "成员" : m.role}
                          </Badge>
                          {m.role !== "owner" && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-muted-foreground hover:text-destructive shrink-0"
                              onClick={(e) => {
                                e.preventDefault();
                                e.stopPropagation();
                                handleRemoveMember(a.id);
                              }}
                              aria-label="移除 Agent"
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                        {(a.modelInfo || a.deviceInfo) && (
                          <div className="mt-2 text-xs text-muted-foreground space-y-0.5">
                            {a.modelInfo && <p>模型: {a.modelInfo}</p>}
                            {a.deviceInfo && <p className="truncate" title={a.deviceInfo}>设备: {a.deviceInfo}</p>}
                          </div>
                        )}
                        {a.lastIP && (
                          <div className="mt-1 text-xs text-muted-foreground">
                            <p>IP: {a.lastIP}</p>
                          </div>
                        )}
                        {a.capabilities && a.capabilities.length > 0 && (
                          <div className="mt-2 flex flex-wrap gap-1">
                            {a.capabilities.map((cap) => (
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

          {/* Create agent dialog */}
          <Dialog open={agentOpen} onOpenChange={setAgentOpen}>
            <DialogContent className="max-w-md">
              <DialogHeader><DialogTitle>新建 Agent</DialogTitle></DialogHeader>
              <form onSubmit={handleCreateAgent} className="space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">名称 <span className="text-destructive">*</span></label>
                  <Input value={newAgentName} onChange={e => setNewAgentName(e.target.value)} placeholder="Agent 名称" required />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">类型 <span className="text-destructive">*</span></label>
                  <Select value={newAgentKind} onValueChange={setNewAgentKind}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="ai">AI</SelectItem>
                      <SelectItem value="human">人类</SelectItem>
                      <SelectItem value="hybrid">混合</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">AI 模型</label>
                  <Input value={newAgentModel} onChange={e => setNewAgentModel(e.target.value)} placeholder="例如: Claude 4 Opus" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">设备信息</label>
                  <Input value={newAgentDevice} onChange={e => setNewAgentDevice(e.target.value)} placeholder="例如: Linux / Chrome 120" />
                </div>
                <div className="flex justify-end gap-2">
                  <Button type="button" variant="outline" onClick={() => setAgentOpen(false)}>取消</Button>
                  <Button type="submit" disabled={agentCreating}>{agentCreating ? "创建中..." : "创建并加入项目"}</Button>
                </div>
              </form>
            </DialogContent>
          </Dialog>

          {/* Token display dialog */}
          <Dialog open={!!createdToken} onOpenChange={(o) => { if (!o) setCreatedToken(null); }}>
            <DialogContent className="max-w-lg">
              <DialogHeader><DialogTitle>Agent 创建成功</DialogTitle></DialogHeader>
              <div className="space-y-4">
                <div className="bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 rounded-md p-3 text-sm text-amber-800 dark:text-amber-200">
                  请立即保存此 Token，关闭后将不再显示。
                </div>
                <div className="flex items-center gap-2">
                  <code className="flex-1 rounded-md border bg-muted px-3 py-2 text-sm font-mono break-all select-all">
                    {createdToken}
                  </code>
                  <Button
                    size="icon"
                    variant="outline"
                    onClick={() => {
                      navigator.clipboard.writeText(createdToken || "");
                      setTokenCopied(true);
                      setTimeout(() => setTokenCopied(false), 2000);
                    }}
                  >
                    {tokenCopied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  </Button>
                </div>

                {/* One-click install */}
                <div className="border-t pt-4">
                  {/* Outer tabs: tool selector */}
                  <div className="flex gap-0 mb-3">
                    <button
                      className={`px-3 py-1.5 text-xs font-medium rounded-l-md border transition-colors ${
                        installTool === "claude"
                          ? "bg-primary text-primary-foreground border-primary"
                          : "bg-muted text-muted-foreground border-border hover:bg-accent"
                      }`}
                      onClick={() => { setInstallTool("claude"); setInstallTab("cli"); }}
                    >
                      Claude Code
                    </button>
                    <button
                      className={`px-3 py-1.5 text-xs font-medium rounded-r-md border border-l-0 transition-colors ${
                        installTool === "opencode"
                          ? "bg-primary text-primary-foreground border-primary"
                          : "bg-muted text-muted-foreground border-border hover:bg-accent"
                      }`}
                      onClick={() => { setInstallTool("opencode"); setInstallTab("cli"); }}
                    >
                      OpenCode
                    </button>
                  </div>

                  {/* Inner tabs: install method */}
                  <div className="flex gap-0 mb-3">
                    <button
                      className={`px-2.5 py-1 text-xs font-medium rounded-l-md border transition-colors ${
                        installTab === "cli"
                          ? "bg-primary text-primary-foreground border-primary"
                          : "bg-muted text-muted-foreground border-border hover:bg-accent"
                      }`}
                      onClick={() => setInstallTab("cli")}
                    >
                      全局一键安装
                    </button>
                    <button
                      className={`px-2.5 py-1 text-xs font-medium border border-l-0 transition-colors ${
                        installTab === "project"
                          ? "bg-primary text-primary-foreground border-primary"
                          : "bg-muted text-muted-foreground border-border hover:bg-accent"
                      }`}
                      onClick={() => setInstallTab("project")}
                    >
                      项目级安装
                    </button>
                    <button
                      className={`px-2.5 py-1 text-xs font-medium rounded-r-md border border-l-0 transition-colors ${
                        installTab === "manual"
                          ? "bg-primary text-primary-foreground border-primary"
                          : "bg-muted text-muted-foreground border-border hover:bg-accent"
                      }`}
                      onClick={() => setInstallTab("manual")}
                    >
                      手动配置
                    </button>
                  </div>

                  {installTool === "claude" && installTab === "cli" && (
                    <ClaudeCLICmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}
                  {installTool === "claude" && installTab === "project" && (
                    <ClaudeProjectCmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}
                  {installTool === "claude" && installTab === "manual" && (
                    <ClaudeManualCmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}
                  {installTool === "opencode" && installTab === "cli" && (
                    <OpenCodeCLICmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}
                  {installTool === "opencode" && installTab === "project" && (
                    <OpenCodeProjectCmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}
                  {installTool === "opencode" && installTab === "manual" && (
                    <OpenCodeManualCmd createdToken={createdToken!} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />
                  )}

                  <p className="text-xs text-muted-foreground mt-1.5">
                    {installTool === "claude" && installTab === "cli" && "复制后在 Claude Code 终端运行，注册到当前用户全局配置"}
                    {installTool === "claude" && installTab === "project" && "复制后在 Claude Code 终端运行，注册到当前项目的 .mcp.json，仅本项目可见"}
                    {installTool === "claude" && installTab === "manual" && "复制后在终端运行，适用于 Claude Code 和 Claude Desktop"}
                    {installTool === "opencode" && installTab === "cli" && "复制后在 OpenCode 终端运行，注册 MCP 服务器"}
                    {installTool === "opencode" && installTab === "project" && "复制后在 OpenCode 终端运行，注册到项目级配置"}
                    {installTool === "opencode" && installTab === "manual" && "复制后在终端运行，OpenCode 会自动加载配置"}
                  </p>
                </div>

                <div className="flex justify-end">
                  <Button onClick={() => setCreatedToken(null)}>关闭</Button>
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* Labels tab */}
      {tab === "labels" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">{labels.length} 个标签</span>
            <Button size="sm" onClick={() => setLabelOpen(true)}>
              <Plus className="mr-1 h-4 w-4" />新建标签
            </Button>
          </div>

          {labels.length === 0 ? (
            <EmptyState title="暂无标签" description="创建标签来标记 Issue" />
          ) : (
            <div className="space-y-2">
              {labels.map((label) => (
                <div key={label.id} className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2">
                  <div
                    className="h-4 w-4 rounded-full shrink-0"
                    style={{ backgroundColor: label.color || "#888" }}
                  />
                  <span className="text-sm flex-1">{label.name}</span>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 text-muted-foreground hover:text-destructive"
                    onClick={() => handleDeleteLabel(label.id)}
                    aria-label="删除标签"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}

          {/* Create label dialog */}
          <Dialog open={labelOpen} onOpenChange={setLabelOpen}>
            <DialogContent>
              <DialogHeader><DialogTitle>新建标签</DialogTitle></DialogHeader>
              <form onSubmit={handleCreateLabel} className="space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">名称</label>
                  <Input value={newLabelName} onChange={e => setNewLabelName(e.target.value)} placeholder="标签名称" required />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">颜色</label>
                  <div className="flex flex-wrap gap-2">
                    {PRESET_COLORS.map((c) => (
                      <button
                        key={c}
                        type="button"
                        className={`h-7 w-7 rounded-full border-2 ${newLabelColor === c ? "border-foreground" : "border-transparent"}`}
                        style={{ backgroundColor: c }}
                        onClick={() => setNewLabelColor(c)}
                      />
                    ))}
                  </div>
                </div>
                <div className="flex justify-end gap-2">
                  <Button type="button" variant="outline" onClick={() => setLabelOpen(false)}>取消</Button>
                  <Button type="submit" disabled={labelCreating}>{labelCreating ? "创建中..." : "创建"}</Button>
                </div>
              </form>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* Milestones tab */}
      {tab === "milestones" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">{milestones.length} 个里程碑</span>
            <Button size="sm" onClick={() => setMsOpen(true)}>
              <Plus className="mr-1 h-4 w-4" />新建里程碑
            </Button>
          </div>

          {milestones.length === 0 ? (
            <EmptyState title="暂无里程碑" description="创建里程碑来规划版本" />
          ) : (
            <div className="space-y-2">
              {milestones.map((ms) => (
                <Card key={ms.id}>
                  <CardContent className="p-4">
                    <div className="flex items-start justify-between">
                      <div>
                        <h3 className="font-medium">{ms.title}</h3>
                        {ms.description && (
                          <p className="mt-1 text-sm text-muted-foreground">{ms.description}</p>
                        )}
                        <div className="mt-2 flex items-center gap-2">
                          <Badge variant="secondary">{ms.state === "open" ? "进行中" : ms.state}</Badge>
                          {ms.dueDate && (
                            <span className="text-xs text-muted-foreground">
                              截止: {new Date(ms.dueDate).toLocaleDateString("zh-CN")}
                            </span>
                          )}
                        </div>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-muted-foreground hover:text-destructive"
                        onClick={() => handleDeleteMilestone(ms.id)}
                        aria-label="删除里程碑"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}

          {/* Create milestone dialog */}
          <Dialog open={msOpen} onOpenChange={setMsOpen}>
            <DialogContent>
              <DialogHeader><DialogTitle>新建里程碑</DialogTitle></DialogHeader>
              <form onSubmit={handleCreateMilestone} className="space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">标题</label>
                  <Input value={newMsTitle} onChange={e => setNewMsTitle(e.target.value)} placeholder="里程碑标题" required />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">描述（可选）</label>
                  <Input value={newMsDesc} onChange={e => setNewMsDesc(e.target.value)} placeholder="描述" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">截止日期（可选）</label>
                  <Input type="date" value={newMsDue} onChange={e => setNewMsDue(e.target.value)} />
                </div>
                <div className="flex justify-end gap-2">
                  <Button type="button" variant="outline" onClick={() => setMsOpen(false)}>取消</Button>
                  <Button type="submit" disabled={msCreating}>{msCreating ? "创建中..." : "创建"}</Button>
                </div>
              </form>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* Members tab */}
      {tab === "members" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">{members.length} 个成员</span>
            {nonMemberAgents.length > 0 && (
              <Button size="sm" onClick={() => setAddOpen(true)}>
                <Plus className="mr-1 h-4 w-4" />添加成员
              </Button>
            )}
          </div>

          {members.length === 0 ? (
            <EmptyState title="暂无成员" />
          ) : (
            <div className="space-y-2">
              {members.map((m) => (
                <div key={m.agent.id} className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2">
                  <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted text-sm font-medium">
                    {m.agent.name.charAt(0)}
                  </div>
                  <div className="flex-1">
                    <p className="text-sm font-medium">{m.agent.name}</p>
                  </div>
                  <Badge variant="secondary">
                    {m.role === "owner" ? "拥有者" : m.role === "member" ? "成员" : m.role}
                  </Badge>
                  {m.role !== "owner" && (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground hover:text-destructive"
                      onClick={() => handleRemoveMember(m.agent.id)}
                      aria-label="移除成员"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}

          <Dialog open={addOpen} onOpenChange={setAddOpen}>
            <DialogContent>
              <DialogHeader><DialogTitle>添加成员</DialogTitle></DialogHeader>
              <div className="space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">选择 Agent</label>
                  <Select value={selectedAgentId} onValueChange={setSelectedAgentId}>
                    <SelectTrigger><SelectValue placeholder="选择 Agent" /></SelectTrigger>
                    <SelectContent>
                      {nonMemberAgents.map((a) => (
                        <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">角色</label>
                  <Select value={selectedRole} onValueChange={setSelectedRole}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="member">成员</SelectItem>
                      <SelectItem value="owner">拥有者</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex justify-end gap-2">
                  <Button variant="outline" onClick={() => setAddOpen(false)}>取消</Button>
                  <Button onClick={handleAddMember} disabled={adding || !selectedAgentId}>
                    {adding ? "添加中..." : "添加"}
                  </Button>
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* Danger Zone */}
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4">
        <div className="flex items-center gap-2 mb-3">
          <AlertTriangle className="h-5 w-5 text-destructive" />
          <h2 className="text-base font-semibold text-destructive">危险区域</h2>
        </div>
        <p className="text-sm text-muted-foreground mb-3">删除项目后不可恢复，所有 Issue、标签、里程碑和成员关系将被永久删除。</p>
        <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)} disabled={deleting}>
          {deleting ? "删除中..." : "删除项目"}
        </Button>
      </div>

      {/* Delete project confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除项目？</AlertDialogTitle>
            <AlertDialogDescription>
              此操作不可撤销。项目中的所有 Issue、标签、里程碑和成员关系都将被永久删除。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDeleteProject} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/* ─── Install command helper components ─── */

interface CmdProps {
  createdToken: string;
  cmdCopied: boolean;
  setCmdCopied: (v: boolean) => void;
}

function CopyBlock({ cmd, cmdCopied, setCmdCopied }: { cmd: string; cmdCopied: boolean; setCmdCopied: (v: boolean) => void }) {
  return (
    <div className="relative">
      <pre className="rounded-md border bg-muted px-3 py-2.5 text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all select-all leading-relaxed">
        {cmd}
      </pre>
      <Button
        size="icon"
        variant="outline"
        className="absolute top-2 right-2 h-7 w-7"
        onClick={() => {
          navigator.clipboard.writeText(cmd);
          setCmdCopied(true);
          setTimeout(() => setCmdCopied(false), 2000);
        }}
      >
        {cmdCopied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </Button>
    </div>
  );
}

function ClaudeCLICmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const cmd = `claude mcp add --transport http chick ${url} --header "Authorization: Bearer ${createdToken}"`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}

function ClaudeManualCmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const config = JSON.stringify({
    mcpServers: { chick: { type: "url", url, headers: { Authorization: "Bearer " + createdToken } } }
  }, null, 2);
  const cmd = `mkdir -p ~/.claude && cat > ~/.claude/settings.json << 'EOF'\n${config}\nEOF`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}

function OpenCodeCLICmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const cmd = `opencode config set mcpServers.chick '{"type":"url","url":"${url}","headers":{"Authorization":"Bearer ${createdToken}"}}'`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}

function OpenCodeManualCmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const config = JSON.stringify({
    mcpServers: { chick: { type: "url", url, headers: { Authorization: "Bearer " + createdToken } } }
  }, null, 2);
  const cmd = `mkdir -p ~/.config/opencode && cat > ~/.config/opencode/config.json << 'EOF'\n${config}\nEOF`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}

function ClaudeProjectCmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const cmd = `claude mcp add --transport http --scope project chick ${url} --header "Authorization: Bearer ${createdToken}"`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}

function OpenCodeProjectCmd({ createdToken, cmdCopied, setCmdCopied }: CmdProps) {
  const url = `${window.location.origin}/mcp`;
  const config = JSON.stringify({
    mcpServers: { chick: { type: "url", url, headers: { Authorization: "Bearer " + createdToken } } }
  }, null, 2);
  const cmd = `echo '${config}' > .mcp.json`;
  return <CopyBlock cmd={cmd} cmdCopied={cmdCopied} setCmdCopied={setCmdCopied} />;
}
