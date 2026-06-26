import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { CreateProposalDialog } from "@/components/project/CreateProposalDialog";
import { CreateTaskDialog } from "@/components/project/CreateTaskDialog";

interface Agent {
  id: string;
  name: string;
}

interface Task {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
  assignee: Agent | null;
}

interface Proposal {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
  author: Agent;
  reviewer: Agent | null;
  tasks: Task[];
}

const columns = [
  { state: "draft", label: "草稿" },
  { state: "submitted", label: "已提交" },
  { state: "under_review", label: "评审中" },
  { state: "approved", label: "已通过" },
  { state: "in_execution", label: "执行中" },
  { state: "completed", label: "已完成" },
  { state: "rejected", label: "已驳回" },
  { state: "cancelled", label: "已取消" },
];

const priorityColors: Record<string, string> = {
  critical: "bg-red-500",
  high: "bg-orange-500",
  medium: "bg-blue-500",
  low: "bg-gray-400",
};


interface Props {
  projectId: string;
  proposals: Proposal[];
  onRefresh: () => void;
}

export function ProposalBoard({ projectId, proposals, onRefresh }: Props) {
  const [taskDialog, setTaskDialog] = useState<{ open: boolean; proposalId: string }>({ open: false, proposalId: "" });

  const grouped = columns.map((col) => ({
    ...col,
    items: proposals.filter((p) => p.state === col.state),
  }));

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <CreateProposalDialog projectId={projectId} onCreated={onRefresh} />
      </div>

      <div className="flex gap-3 overflow-x-auto pb-2 scrollbar-none">
        {grouped.map((col) => (
          <div key={col.state} className="flex flex-col gap-2 min-w-[220px] w-72 shrink-0">
            <div className="flex items-center justify-between px-1">
              <h3 className="text-sm font-medium text-muted-foreground">{col.label}</h3>
              <span className="text-xs text-muted-foreground">{col.items.length}</span>
            </div>
            {col.items.map((proposal) => (
              <Card key={proposal.id} className="shadow-sm">
                <CardHeader className="p-3 pb-0">
                  <div className="flex items-start justify-between gap-1">
                    <Link to={`/proposals/${proposal.id}`} className="text-sm font-medium hover:underline leading-tight">
                      {proposal.title}
                    </Link>
                    <div className={`w-2 h-2 rounded-full shrink-0 mt-1 ${priorityColors[proposal.priority] || "bg-gray-400"}`} />
                  </div>
                </CardHeader>
                <CardContent className="p-3 pt-2 space-y-1">
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span>#{proposal.number}</span>
                    <span>{proposal.author.name}</span>
                  </div>
                  {(proposal.state === "approved" || proposal.state === "in_execution") && (
                    <div className="flex flex-wrap gap-1 pt-1">
                      {proposal.tasks?.length > 0 ? (
                        proposal.tasks.map((t) => (
                          <Link key={t.id} to={`/tasks/${t.id}`}>
                            <Badge variant="outline" className="text-xs cursor-pointer hover:bg-accent">
                              #{t.number} {t.title}
                            </Badge>
                          </Link>
                        ))
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 text-xs gap-1"
                          onClick={() => setTaskDialog({ open: true, proposalId: proposal.id })}
                        >
                          <Plus className="h-3 w-3" />
                          创建任务
                        </Button>
                      )}
                    </div>
                  )}
                  {/* Show task count for in_execution */}
                  {proposal.state === "in_execution" && proposal.tasks && proposal.tasks.length > 0 && (
                    <div className="pt-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 text-xs gap-1"
                        onClick={() => setTaskDialog({ open: true, proposalId: proposal.id })}
                      >
                        <Plus className="h-3 w-3" />
                        添加任务
                      </Button>
                    </div>
                  )}
                </CardContent>
              </Card>
            ))}
            {col.items.length === 0 && (
              <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">
                暂无
              </div>
            )}
          </div>
        ))}
      </div>

      {taskDialog.open && (
        <CreateTaskDialog
          proposalId={taskDialog.proposalId}
          open={taskDialog.open}
          onOpenChange={(open) => setTaskDialog({ ...taskDialog, open })}
          onCreated={onRefresh}
        />
      )}
    </div>
  );
}
