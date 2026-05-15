import { useState } from "react";
import { Link } from "react-router-dom";
import {
  DndContext,
  DragOverlay,
  useDraggable,
  useDroppable,
  type DragEndEvent,
  type DragStartEvent,
  PointerSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { Badge } from "@/components/ui/badge";
import { GripVertical } from "lucide-react";
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
  startedAt: string | null;
  completedAt: string | null;
}

interface Column {
  state: string;
  label: string;
}

const stateColors: Record<string, string> = {
  open: "border-l-green-500",
  in_progress: "border-l-blue-500",
  blocked: "border-l-amber-500",
  review: "border-l-purple-500",
  pending_confirmation: "border-l-cyan-500",
  later: "border-l-slate-500",
  reopen: "border-l-orange-500",
  closed_completed: "border-l-gray-500",
  closed_not_planned: "border-l-gray-500",
  closed_rejected: "border-l-red-500",
};

const priorityLabels: Record<string, string> = {
  critical: "关键",
  high: "高",
  medium: "中",
  low: "低",
};

const priorityColors: Record<string, string> = {
  critical: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  high: "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200",
  medium: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  low: "bg-sky-100 text-sky-800 dark:bg-sky-900 dark:text-sky-200",
};

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

function LabelsDisplay({ labels, onRemove }: { labels: Label[]; onRemove?: (id: string) => void }) {
  if (!labels || labels.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1">
      {labels.map((l) => (
        <span
          key={l.id}
          className={`inline-block rounded px-1.5 py-0.5 text-[10px] font-medium leading-tight ${onRemove ? "cursor-pointer hover:opacity-70" : ""}`}
          style={{ backgroundColor: l.color ? `${l.color}25` : undefined, color: l.color || undefined }}
          onClick={onRemove ? (e) => { e.preventDefault(); e.stopPropagation(); onRemove(l.id); } : undefined}
          title={onRemove ? "点击移除" : undefined}
        >
          {l.name}
        </span>
      ))}
    </div>
  );
}

function DraggableIssue({
  issue,
}: {
  issue: Issue;
}) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: `issue-${issue.id}`,
    data: { issue },
  });

  const style = transform
    ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` }
    : undefined;

  return (
    <Link
      ref={setNodeRef}
      to={`/issues/${issue.id}`}
      style={style}
      className={`block rounded-lg border bg-card p-3 border-l-2 transition-colors hover:bg-accent ${
        stateColors[issue.state] || "border-l-border"
      } ${isDragging ? "opacity-50 z-50" : ""}`}
      {...attributes}
      {...listeners}
    >
      <div className="flex items-center gap-1 text-xs text-muted-foreground">
        <GripVertical className="h-3 w-3 shrink-0 opacity-40" />
        <span>#{issue.number}</span>
        <Badge className={`text-xs ${priorityColors[issue.priority] || ""}`}>
          {priorityLabels[issue.priority] || issue.priority}
        </Badge>
      </div>
      <p className="mt-1 text-sm line-clamp-2">{issue.title}</p>
      {issue.assignees && issue.assignees.length > 0 && (
        <div className="mt-1.5 flex items-center gap-1.5">
          {issue.assignees.slice(0, 3).map((a) => (
            <span key={a.agent.id} className="inline-flex items-center justify-center h-5 w-5 rounded-full bg-primary/10 text-[10px] font-medium text-primary" title={a.agent.name}>
              {a.agent.name.charAt(0)}
            </span>
          ))}
          {issue.assignees.length > 3 && (
            <span className="text-[10px] text-muted-foreground">+{issue.assignees.length - 3}</span>
          )}
        </div>
      )}
      <LabelsDisplay labels={issue.labels} />
      {issue.milestone && (
        <p className="mt-1 text-[10px] text-muted-foreground truncate">⛳ {issue.milestone.title}</p>
      )}
      {issue.startedAt && (
        <p className="mt-1 text-[10px] text-muted-foreground">开始 {new Date(issue.startedAt).toLocaleDateString()}</p>
      )}
      {issue.completedAt && (
        <p className="mt-1 text-[10px] text-muted-foreground">完成 {new Date(issue.completedAt).toLocaleDateString()}</p>
      )}
    </Link>
  );
}

function SimpleIssueCard({
  issue,
  onTransition,
  projectLabels,
  projectMilestones,
  onAddLabel,
  onRemoveLabel,
  onChangeMilestone,
  onCreateLabel,
  onCreateMilestone,
  validTransitions,
}: {
  issue: Issue;
  onTransition: (issueId: string, toState: string, note?: string) => Promise<void>;
  projectLabels: Label[];
  projectMilestones: Milestone[];
  onAddLabel: (issueId: string, labelId: string) => Promise<void>;
  onRemoveLabel: (issueId: string, labelId: string) => Promise<void>;
  onChangeMilestone: (issueId: string, milestoneId: string) => Promise<void>;
  onCreateLabel: (name: string, color: string) => Promise<Label | null>;
  onCreateMilestone: (title: string) => Promise<Milestone | null>;
  validTransitions: Record<string, string[]>;
}) {
  const [moving, setMoving] = useState(false);
  const [showLabels, setShowLabels] = useState(false);
  const [showMilestones, setShowMilestones] = useState(false);
  const [creatingLabel, setCreatingLabel] = useState(false);
  const [creatingMilestone, setCreatingMilestone] = useState(false);
  const [newLabelName, setNewLabelName] = useState("");
  const [newLabelColor, setNewLabelColor] = useState("#6366f1");
  const [newMilestoneTitle, setNewMilestoneTitle] = useState("");

  const handleQuickMove = async (e: React.MouseEvent, toState: string) => {
    e.preventDefault();
    e.stopPropagation();
    const note = window.prompt(`请输入「${stateLabels[toState]}」的备注说明（可选）：`);
    if (note === null) return; // cancelled
    setMoving(true);
    try {
      await onTransition(issue.id, toState, note);
      toast.success(`已移至 ${stateLabels[toState]}`);
    } catch {
      toast.error("状态变更失败");
    } finally {
      setMoving(false);
    }
  };

  const transitions = validTransitions[issue.state] || [];
  const availableLabels = projectLabels.filter((pl) => !(issue.labels || []).some((l) => l.id === pl.id));

  const stopProp = (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); };

  const handleCreateLabelWrap = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!newLabelName.trim()) return;
    const label = await onCreateLabel(newLabelName.trim(), newLabelColor);
    if (label) {
      await onAddLabel(issue.id, label.id);
      setCreatingLabel(false);
      setNewLabelName("");
    }
  };

  const handleCreateMilestoneWrap = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!newMilestoneTitle.trim()) return;
    const ms = await onCreateMilestone(newMilestoneTitle.trim());
    if (ms) {
      await onChangeMilestone(issue.id, ms.id);
      setCreatingMilestone(false);
      setNewMilestoneTitle("");
    }
  };

  return (
    <Link
      to={`/issues/${issue.id}`}
      className={`block rounded-lg border bg-card border-l-2 transition-colors hover:bg-accent ${
        stateColors[issue.state] || "border-l-border"
      }`}
    >
      <div className="p-3">
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <span>#{issue.number}</span>
          <Badge className={`text-xs ${priorityColors[issue.priority] || ""}`}>
            {priorityLabels[issue.priority] || issue.priority}
          </Badge>
        </div>
        <p className="mt-1 text-sm line-clamp-2">{issue.title}</p>
        {issue.assignees && issue.assignees.length > 0 && (
          <div className="mt-1.5 flex items-center gap-1.5">
            {issue.assignees.slice(0, 3).map((a) => (
              <span key={a.agent.id} className="inline-flex items-center justify-center h-5 w-5 rounded-full bg-primary/10 text-[10px] font-medium text-primary" title={a.agent.name}>
                {a.agent.name.charAt(0)}
              </span>
            ))}
            {issue.assignees.length > 3 && (
              <span className="text-[10px] text-muted-foreground">+{issue.assignees.length - 3}</span>
            )}
          </div>
        )}

        {/* Labels */}
        <div className="mt-1.5 flex flex-wrap items-center gap-1" onClick={stopProp}>
          <LabelsDisplay labels={issue.labels} onRemove={(lid) => onRemoveLabel(issue.id, lid)} />
          <button
            className="text-[10px] text-muted-foreground hover:text-foreground"
            onClick={(e) => { e.preventDefault(); e.stopPropagation(); setShowLabels(!showLabels); }}
          >
            {showLabels ? "收起" : "+标签"}
          </button>
        </div>
        {showLabels && (
          <div className="mt-1 space-y-1.5 border-t pt-1.5" onClick={stopProp}>
            {availableLabels.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {availableLabels.map((l) => (
                  <span
                    key={l.id}
                    className="inline-block cursor-pointer rounded px-1.5 py-0.5 text-[10px] font-medium border hover:bg-accent"
                    style={{ borderColor: l.color || undefined, color: l.color || undefined }}
                    onClick={async (e) => { e.preventDefault(); e.stopPropagation(); await onAddLabel(issue.id, l.id); }}
                  >
                    + {l.name}
                  </span>
                ))}
              </div>
            )}
            {creatingLabel ? (
              <div className="flex items-center gap-1">
                <input
                  value={newLabelName}
                  onChange={(e) => setNewLabelName(e.target.value)}
                  placeholder="标签名"
                  className="h-6 w-20 rounded border bg-transparent px-1 text-[10px]"
                  onKeyDown={(e) => { if (e.key === "Enter") handleCreateLabelWrap(e as any); }}
                />
                <input
                  type="color"
                  value={newLabelColor}
                  onChange={(e) => setNewLabelColor(e.target.value)}
                  className="h-6 w-8 rounded border cursor-pointer"
                />
                <button className="text-[10px] text-primary font-medium hover:underline" onClick={handleCreateLabelWrap} disabled={!newLabelName.trim()}>创建</button>
                <button className="text-[10px] text-muted-foreground hover:underline" onClick={(e) => { e.preventDefault(); e.stopPropagation(); setCreatingLabel(false); }}>取消</button>
              </div>
            ) : (
              <button
                className="text-[10px] text-primary hover:underline"
                onClick={(e) => { e.preventDefault(); e.stopPropagation(); setCreatingLabel(true); }}
              >
                创建标签
              </button>
            )}
          </div>
        )}

        {/* Milestone */}
        <div className="mt-1 flex items-center gap-1" onClick={stopProp}>
          {issue.milestone ? (
            <span className="text-[10px] text-muted-foreground truncate">⛳ {issue.milestone.title}</span>
          ) : null}
          <button
            className="text-[10px] text-muted-foreground hover:text-foreground"
            onClick={(e) => { e.preventDefault(); e.stopPropagation(); setShowMilestones(!showMilestones); }}
          >
            {showMilestones ? "收起" : issue.milestone ? "更换" : "+里程碑"}
          </button>
        </div>
        {showMilestones && (
          <div className="mt-1 space-y-1.5 border-t pt-1.5" onClick={stopProp}>
            <button
              className="block w-full text-left rounded px-1.5 py-0.5 text-[10px] hover:bg-accent text-muted-foreground"
              onClick={async (e) => { e.preventDefault(); e.stopPropagation(); await onChangeMilestone(issue.id, ""); setShowMilestones(false); }}
            >
              不关联
            </button>
            {projectMilestones.map((m) => (
              <button
                key={m.id}
                className={`block w-full text-left rounded px-1.5 py-0.5 text-[10px] hover:bg-accent ${issue.milestone?.id === m.id ? "bg-primary/10 font-medium" : ""}`}
                onClick={async (e) => { e.preventDefault(); e.stopPropagation(); await onChangeMilestone(issue.id, m.id); setShowMilestones(false); }}
              >
                {m.title}
              </button>
            ))}
            {creatingMilestone ? (
              <div className="flex items-center gap-1 pt-1">
                <input
                  value={newMilestoneTitle}
                  onChange={(e) => setNewMilestoneTitle(e.target.value)}
                  placeholder="里程碑名称"
                  className="h-6 w-28 rounded border bg-transparent px-1 text-[10px]"
                  onKeyDown={(e) => { if (e.key === "Enter") handleCreateMilestoneWrap(e as any); }}
                />
                <button className="text-[10px] text-primary font-medium hover:underline" onClick={handleCreateMilestoneWrap} disabled={!newMilestoneTitle.trim()}>创建</button>
                <button className="text-[10px] text-muted-foreground hover:underline" onClick={(e) => { e.preventDefault(); e.stopPropagation(); setCreatingMilestone(false); }}>取消</button>
              </div>
            ) : (
              <button
                className="text-[10px] text-primary hover:underline"
                onClick={(e) => { e.preventDefault(); e.stopPropagation(); setCreatingMilestone(true); }}
              >
                创建里程碑
              </button>
            )}
          </div>
        )}
      </div>
      {issue.startedAt && (
        <div className="px-3 pb-1">
          <p className="text-[10px] text-muted-foreground">开始 {new Date(issue.startedAt).toLocaleDateString()}</p>
        </div>
      )}
      {issue.completedAt && (
        <div className="px-3 pb-1">
          <p className="text-[10px] text-muted-foreground">完成 {new Date(issue.completedAt).toLocaleDateString()}</p>
        </div>
      )}
      {transitions.length > 0 && (
        <div
          className="flex gap-1 border-t px-3 py-1.5 overflow-x-auto scrollbar-none"
          onClick={(e) => e.stopPropagation()}
          onPointerDown={(e) => e.stopPropagation()}
        >
          {transitions.map((s) => (
            <button
              key={s}
              onClick={(e) => handleQuickMove(e, s)}
              disabled={moving}
              className="shrink-0 rounded-md px-2 py-1 text-[11px] font-medium bg-accent text-accent-foreground hover:bg-primary hover:text-primary-foreground transition-colors disabled:opacity-50 min-h-[28px]"
            >
              {stateLabels[s]}
            </button>
          ))}
        </div>
      )}
    </Link>
  );
}

function DroppableColumn({
  column,
  issues,
}: {
  column: Column;
  issues: Issue[];
}) {
  const { setNodeRef, isOver } = useDroppable({
    id: column.state,
    data: { column },
  });

  return (
    <div
      ref={setNodeRef}
      className={`space-y-2 rounded-lg p-2 transition-colors ${
        isOver ? "bg-accent/50 ring-2 ring-primary/30" : ""
      }`}
    >
      <h2 className="flex items-center gap-2 text-sm font-medium px-1">
        <span className="text-muted-foreground">{column.label}</span>
        <Badge variant="secondary" className="text-xs">
          {issues.length}
        </Badge>
      </h2>
      {issues.length === 0 && (
        <p className="py-4 text-center text-xs text-muted-foreground">无</p>
      )}
      {issues.map((issue) => (
        <DraggableIssue key={issue.id} issue={issue} />
      ))}
    </div>
  );
}

function StaticColumn({
  column,
  issues,
  onTransition,
  projectLabels,
  projectMilestones,
  onAddLabel,
  onRemoveLabel,
  onChangeMilestone,
  onCreateLabel,
  onCreateMilestone,
  validTransitions,
}: {
  column: Column;
  issues: Issue[];
  onTransition: (issueId: string, toState: string, note?: string) => Promise<void>;
  projectLabels: Label[];
  projectMilestones: Milestone[];
  onAddLabel: (issueId: string, labelId: string) => Promise<void>;
  onRemoveLabel: (issueId: string, labelId: string) => Promise<void>;
  onChangeMilestone: (issueId: string, milestoneId: string) => Promise<void>;
  onCreateLabel: (name: string, color: string) => Promise<Label | null>;
  onCreateMilestone: (title: string) => Promise<Milestone | null>;
  validTransitions: Record<string, string[]>;
}) {
  return (
    <div className="space-y-2 rounded-lg p-2">
      <h2 className="flex items-center gap-2 text-sm font-medium px-1">
        <span className="text-muted-foreground">{column.label}</span>
        <Badge variant="secondary" className="text-xs">
          {issues.length}
        </Badge>
      </h2>
      {issues.length === 0 && (
        <p className="py-4 text-center text-xs text-muted-foreground">无</p>
      )}
      {issues.map((issue) => (
        <SimpleIssueCard
          key={issue.id}
          issue={issue}
          onTransition={onTransition}
          projectLabels={projectLabels}
          projectMilestones={projectMilestones}
          onAddLabel={onAddLabel}
          onRemoveLabel={onRemoveLabel}
          onChangeMilestone={onChangeMilestone}
          onCreateLabel={onCreateLabel}
          onCreateMilestone={onCreateMilestone}
          validTransitions={validTransitions}
        />
      ))}
    </div>
  );
}

function DragOverlayCard({ issue }: { issue: Issue }) {
  return (
    <div
      className={`rounded-lg border bg-card p-3 border-l-2 shadow-xl rotate-3 ${
        stateColors[issue.state] || "border-l-border"
      }`}
    >
      <div className="flex items-center gap-1 text-xs text-muted-foreground">
        <span>#{issue.number}</span>
        <Badge className={`text-xs ${priorityColors[issue.priority] || ""}`}>
          {priorityLabels[issue.priority] || issue.priority}
        </Badge>
      </div>
      <p className="mt-1 text-sm line-clamp-2">{issue.title}</p>
    </div>
  );
}

interface IssueBoardProps {
  columns: Column[];
  issues: Issue[];
  isDesktop: boolean;
  onTransition: (issueId: string, toState: string, note?: string) => Promise<void>;
  projectLabels: Label[];
  projectMilestones: Milestone[];
  onAddLabel: (issueId: string, labelId: string) => Promise<void>;
  onRemoveLabel: (issueId: string, labelId: string) => Promise<void>;
  onChangeMilestone: (issueId: string, milestoneId: string) => Promise<void>;
  onCreateLabel: (name: string, color: string) => Promise<Label | null>;
  onCreateMilestone: (title: string) => Promise<Milestone | null>;
  validTransitions: Record<string, string[]>;
}

export function IssueBoard({
  columns,
  issues,
  isDesktop,
  onTransition,
  projectLabels,
  projectMilestones,
  onAddLabel,
  onRemoveLabel,
  onChangeMilestone,
  onCreateLabel,
  onCreateMilestone,
  validTransitions,
}: IssueBoardProps) {
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 150, tolerance: 8 } })
  );

  const handleDragStart = (event: DragStartEvent) => {
    const data = event.active.data.current as { issue: Issue } | undefined;
    if (data) setActiveIssue(data.issue);
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    setActiveIssue(null);

    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const activeData = active.data.current as { issue: Issue } | undefined;
    if (!activeData) return;

    const overData = over.data.current as { column: Column } | undefined;
    if (!overData) return;

    const fromState = activeData.issue.state;
    const toState = overData.column.state;
    if (fromState === toState) return;

    const note = window.prompt(`请输入拖拽到「${overData.column.label}」的备注说明（可选）：`);
    if (note === null) return;

    try {
      await onTransition(activeData.issue.id, toState, note);
    } catch {
      toast.error("状态变更失败");
    }
  };

  const grouped = columns.map((col) => ({
    ...col,
    items: issues.filter((i) => i.state === col.state),
  }));

  if (isDesktop) {
    return (
      <DndContext
        sensors={sensors}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <div className="flex gap-3 overflow-x-auto pb-2">
          {grouped.map((col) => (
            <div key={col.state} className="min-w-[260px] w-72 shrink-0">
              <DroppableColumn
                column={col}
                issues={col.items}
              />
            </div>
          ))}
        </div>

        <DragOverlay>
          {activeIssue ? <DragOverlayCard issue={activeIssue} /> : null}
        </DragOverlay>
      </DndContext>
    );
  }

  return (
    <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory scrollbar-none">
      {grouped.map((col) => (
        <div key={col.state} className="min-w-[300px] w-[85vw] snap-start shrink-0">
          <StaticColumn
            column={col}
            issues={col.items}
            onTransition={onTransition}
            projectLabels={projectLabels}
            projectMilestones={projectMilestones}
            onAddLabel={onAddLabel}
            onRemoveLabel={onRemoveLabel}
            onChangeMilestone={onChangeMilestone}
            onCreateLabel={onCreateLabel}
            onCreateMilestone={onCreateMilestone}
            validTransitions={validTransitions}
          />
        </div>
      ))}
    </div>
  );
}
