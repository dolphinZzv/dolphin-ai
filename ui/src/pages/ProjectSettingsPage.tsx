import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { gql } from "@/lib/graphql";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { ErrorFallback } from "@/components/shared/ErrorFallback";
import { EmptyState } from "@/components/shared/EmptyState";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

interface Label {
  id: string;
  name: string;
  color: string | null;
}

interface Skill {
  id: string;
  name: string;
  description: string | null;
  definition: string;
}

interface Member {
  agent: { id: string; name: string };
  role: string;
}

interface AgentBrief {
  id: string;
  name: string;
}

export function ProjectSettingsPage() {
  const { id } = useParams<{ id: string }>();
  const [tab, setTab] = useState<"labels" | "milestones" | "skills" | "members">("labels");
  const [labels, setLabels] = useState<Label[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // add member dialog
  const [addOpen, setAddOpen] = useState(false);
  const [allAgents, setAllAgents] = useState<AgentBrief[]>([]);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [selectedRole, setSelectedRole] = useState("member");
  const [adding, setAdding] = useState(false);

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
        `query project($id: ID!) {
          project(id: $id) {
            skills { id name description definition }
            members { agent { id name } role }
          }
        }`,
        { id }
      ),
      gql("query agents { agents { id name } }"),
    ])
      .then(([lJson, pJson, aJson]) => {
        if (lJson.errors) { setError(lJson.errors[0].message); return; }
        if (pJson.errors) { setError(pJson.errors[0].message); return; }
        if (aJson.errors) { setError(aJson.errors[0].message); return; }
        setLabels(lJson.data.labels);
        setSkills(pJson.data.project.skills || []);
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

  const tabs = [
    { key: "labels" as const, label: "标签" },
    { key: "milestones" as const, label: "里程碑" },
    { key: "skills" as const, label: "技能" },
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

      {/* Labels tab */}
      {tab === "labels" && (
        <div className="space-y-2">
          {labels.length === 0 ? (
            <EmptyState title="暂无标签" />
          ) : (
            labels.map((label) => (
              <div key={label.id} className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2">
                <div
                  className="h-4 w-4 rounded-full"
                  style={{ backgroundColor: label.color || "#888" }}
                />
                <span className="text-sm">{label.name}</span>
              </div>
            ))
          )}
        </div>
      )}

      {/* Milestones tab */}
      {tab === "milestones" && (
        <EmptyState title="暂无里程碑" />
      )}

      {/* Skills tab */}
      {tab === "skills" && (
        <div className="space-y-2">
          {skills.length === 0 ? (
            <EmptyState title="暂无技能" description="项目尚未配置技能" />
          ) : (
            skills.map((skill) => (
              <Card key={skill.id}>
                <CardContent className="p-4">
                  <div className="flex items-start justify-between">
                    <h3 className="font-medium">{skill.name}</h3>
                  </div>
                  {skill.description && (
                    <p className="mt-1 text-sm text-muted-foreground">{skill.description}</p>
                  )}
                  {skill.definition && (
                    <div className="mt-2 rounded-md bg-muted p-2">
                      <pre className="text-xs overflow-x-auto">{skill.definition}</pre>
                    </div>
                  )}
                </CardContent>
              </Card>
            ))
          )}
        </div>
      )}

      {/* Members tab */}
      {tab === "members" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">
              {members.length} 个成员
            </span>
            {nonMemberAgents.length > 0 && (
              <Button size="sm" onClick={() => setAddOpen(true)}>
                <Plus className="mr-1 h-4 w-4" />
                添加成员
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

          {/* Add member dialog */}
          <Dialog open={addOpen} onOpenChange={setAddOpen}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>添加成员</DialogTitle>
              </DialogHeader>
              <div className="space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">选择 Agent</label>
                  <Select value={selectedAgentId} onValueChange={setSelectedAgentId}>
                    <SelectTrigger>
                      <SelectValue placeholder="选择 Agent" />
                    </SelectTrigger>
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
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
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
    </div>
  );
}
