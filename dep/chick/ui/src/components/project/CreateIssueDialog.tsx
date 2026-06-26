import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Plus, ChevronDown, ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { gql } from "@/lib/graphql";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";

interface Label {
  id: string;
  name: string;
  color: string | null;
}

interface Milestone {
  id: string;
  title: string;
}

const createIssueSchema = z.object({
  title: z.string().min(1, "标题不能为空").max(200, "标题不能超过 200 字"),
  description: z.string().optional(),
  priority: z.string().min(1, "请选择优先级"),
  environment: z.string().optional(),
  branch: z.string().optional(),
  link: z.string().optional(),
});

type CreateIssueForm = z.infer<typeof createIssueSchema>;

interface CreateIssueDialogProps {
  projectId: string;
  onCreated: () => void;
}

const priorityOptions = [
  { value: "critical", label: "关键", color: "text-red-600 dark:text-red-400" },
  { value: "high", label: "高", color: "text-orange-600 dark:text-orange-400" },
  { value: "medium", label: "中", color: "text-yellow-600 dark:text-yellow-400" },
  { value: "low", label: "低", color: "text-green-600 dark:text-green-400" },
] as const;

export function CreateIssueDialog({
  projectId,
  onCreated,
}: CreateIssueDialogProps) {
  const [open, setOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [labels, setLabels] = useState<Label[]>([]);
  const [milestones, setMilestones] = useState<Milestone[]>([]);
  const [selectedLabelIds, setSelectedLabelIds] = useState<string[]>([]);
  const [selectedMilestoneId, setSelectedMilestoneId] = useState<string>("");
  const [showAdvanced, setShowAdvanced] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<CreateIssueForm>({
    resolver: zodResolver(createIssueSchema),
    defaultValues: { title: "", description: "", priority: "medium", environment: "", branch: "", link: "" },
  });

  const currentPriority = watch("priority");

  useEffect(() => {
    if (!open) return;
    setSelectedLabelIds([]);
    setSelectedMilestoneId("");
    setShowAdvanced(false);
    setError("");
    Promise.all([
      gql(
        `query labels($projectId: ID!) { labels(projectID: $projectId) { id name color } }`,
        { projectId }
      ),
      gql(
        `query milestones($projectId: ID!) { milestones(projectID: $projectId) { id title } }`,
        { projectId }
      ),
    ]).then(([lJson, mJson]) => {
      if (!lJson.errors) setLabels(lJson.data.labels);
      if (!mJson.errors) setMilestones(mJson.data.milestones);
    });
  }, [open, projectId]);

  const toggleLabel = (id: string) => {
    setSelectedLabelIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  };

  const onSubmit = async (data: CreateIssueForm) => {
    setSubmitting(true);
    setError("");

    try {
      const json = await gql(
        `mutation createIssue($pid: ID!, $title: String!, $description: String, $priority: Priority!, $labelIDs: [ID!], $milestoneId: ID, $environment: String, $branch: String, $link: String) {
          createIssue(projectID: $pid, title: $title, description: $description, priority: $priority, labelIDs: $labelIDs, milestoneId: $milestoneId, environment: $environment, branch: $branch, link: $link) { id number title }
        }`,
        {
          pid: projectId,
          title: data.title,
          description: data.description || null,
          priority: data.priority,
          labelIDs: selectedLabelIds.length > 0 ? selectedLabelIds : null,
          milestoneId: selectedMilestoneId || null,
          environment: data.environment || null,
          branch: data.branch || null,
          link: data.link || null,
        }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        toast.error(json.errors[0].message);
        return;
      }
      reset();
      setOpen(false);
      toast.success("Issue 创建成功");
      onCreated();
    } catch {
      setError("网络错误，请重试");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className="mr-1 h-4 w-4" />
          创建 Issue
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl gap-0 p-0">
        <DialogHeader className="px-6 pt-6 pb-0">
          <DialogTitle className="sr-only">创建 Issue</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit)} className="max-h-[80vh] overflow-y-auto">
          {/* Title — hero */}
          <div className="px-6 pt-4 pb-3">
            <input
              id="title"
              autoFocus
              placeholder="Issue 标题"
              className="w-full text-xl font-semibold placeholder:text-muted-foreground/40 bg-transparent border-none outline-none focus:ring-0"
              {...register("title")}
            />
            {errors.title && (
              <p className="mt-1 text-xs text-destructive">{errors.title.message}</p>
            )}
          </div>

          {/* Description */}
          <div className="px-6 pb-4">
            <textarea
              id="description"
              placeholder="描述 — 支持 Markdown 格式..."
              rows={5}
              className="w-full resize-none text-sm leading-relaxed placeholder:text-muted-foreground/40 bg-transparent border-none outline-none focus:ring-0"
              {...register("description")}
            />
          </div>

          {/* Divider */}
          <div className="border-t" />

          {/* Metadata section */}
          <div className="px-6 py-4 space-y-4">
            {/* Priority — segmented control */}
            <div className="flex items-center gap-3">
              <span className="text-xs font-medium text-muted-foreground w-12 shrink-0">优先级</span>
              <div className="flex rounded-lg border p-0.5 bg-muted/50">
                {priorityOptions.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setValue("priority", opt.value)}
                    className={`relative px-3 py-1.5 text-xs font-medium rounded-md transition-all ${
                      currentPriority === opt.value
                        ? "bg-background shadow-sm text-foreground"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
              {currentPriority && (
                <span className={`text-[10px] font-medium ${priorityOptions.find(p => p.value === currentPriority)?.color}`}>
                  {currentPriority === "critical" ? "P0" : currentPriority === "high" ? "P1" : currentPriority === "medium" ? "P2" : "P3"}
                </span>
              )}
            </div>

            {/* Labels */}
            <div className="flex items-start gap-3">
              <span className="text-xs font-medium text-muted-foreground w-12 shrink-0 mt-0.5">标签</span>
              <div className="flex flex-wrap gap-1.5 min-h-6">
                {labels.length === 0 ? (
                  <span className="text-xs text-muted-foreground/50">暂无标签</span>
                ) : (
                  labels.map((l) => (
                    <Badge
                      key={l.id}
                      variant={selectedLabelIds.includes(l.id) ? "default" : "outline"}
                      className="cursor-pointer text-xs font-normal select-none"
                      style={
                        selectedLabelIds.includes(l.id)
                          ? {
                              backgroundColor: l.color || undefined,
                              color: "#fff",
                              borderColor: "transparent",
                            }
                          : {
                              borderColor: l.color || undefined,
                              color: l.color || undefined,
                            }
                      }
                      onClick={() => toggleLabel(l.id)}
                    >
                      {l.name}
                    </Badge>
                  ))
                )}
              </div>
            </div>

            {/* Milestone */}
            <div className="flex items-center gap-3">
              <span className="text-xs font-medium text-muted-foreground w-12 shrink-0">里程碑</span>
              <Select value={selectedMilestoneId || "_none"} onValueChange={(v) => setSelectedMilestoneId(v === "_none" ? "" : v)}>
                <SelectTrigger className="h-8 text-xs w-48">
                  <SelectValue placeholder="不关联" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_none">不关联</SelectItem>
                  {milestones.map((m) => (
                    <SelectItem key={m.id} value={m.id}>{m.title}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Advanced options toggle */}
            <div>
              <button
                type="button"
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                {showAdvanced ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                高级选项
              </button>
              {showAdvanced && (
                <div className="mt-3 space-y-3 pl-0">
                  <div className="flex items-center gap-3">
                    <span className="text-xs font-medium text-muted-foreground w-12 shrink-0">环境</span>
                    <Input placeholder="例如: staging, production" className="h-8 text-xs" {...register("environment")} />
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-xs font-medium text-muted-foreground w-12 shrink-0">分支</span>
                    <Input placeholder="分支名称" className="h-8 text-xs" {...register("branch")} />
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-xs font-medium text-muted-foreground w-12 shrink-0">链接</span>
                    <Input placeholder="相关链接" className="h-8 text-xs" {...register("link")} />
                  </div>
                </div>
              )}
            </div>
          </div>

          {error && (
            <p className="px-6 pb-2 text-xs text-destructive" role="alert">
              {error}
            </p>
          )}

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 border-t px-6 py-3">
            <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "创建中..." : "创建 Issue"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
