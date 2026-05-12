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

interface Issue {
  id: string;
  number: number;
  title: string;
  state: string;
  priority: string;
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
  closed_completed: "border-l-gray-500",
  closed_not_planned: "border-l-gray-500",
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
  onTransition: (issueId: string, toState: string) => Promise<void>;
}

export function IssueBoard({
  columns,
  issues,
  isDesktop,
  onTransition,
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

    try {
      await onTransition(activeData.issue.id, toState);
    } catch {
      toast.error("状态变更失败");
    }
  };

  const grouped = columns.map((col) => ({
    ...col,
    items: issues.filter((i) => i.state === col.state),
  }));

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className={`grid gap-3 ${isDesktop ? "grid-cols-4" : "grid-cols-1 sm:grid-cols-2"}`}>
        {grouped.map((col) => (
          <DroppableColumn
            key={col.state}
            column={col}
            issues={col.items}
          />
        ))}
      </div>

      <DragOverlay>
        {activeIssue ? <DragOverlayCard issue={activeIssue} /> : null}
      </DragOverlay>
    </DndContext>
  );
}
