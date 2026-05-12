import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Plus } from "lucide-react";
import { toast } from "sonner";

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
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";

const createIssueSchema = z.object({
  title: z.string().min(1, "标题不能为空").max(200, "标题不能超过 200 字"),
  description: z.string().optional(),
  priority: z.string().min(1, "请选择优先级"),
});

type CreateIssueForm = z.infer<typeof createIssueSchema>;

interface CreateIssueDialogProps {
  projectId: string;
  creatorId: string;
  onCreated: () => void;
}

export function CreateIssueDialog({
  projectId,
  creatorId,
  onCreated,
}: CreateIssueDialogProps) {
  const [open, setOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const {
    register,
    handleSubmit,
    setValue,
    reset,
    formState: { errors },
  } = useForm<CreateIssueForm>({
    resolver: zodResolver(createIssueSchema),
    defaultValues: { title: "", description: "", priority: "medium" },
  });

  const token = localStorage.getItem("token");
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const onSubmit = async (data: CreateIssueForm) => {
    setSubmitting(true);
    setError("");

    try {
      const res = await fetch("/graphql", {
        method: "POST",
        headers,
        body: JSON.stringify({
          operationName: "createIssue",
          query: `mutation createIssue($pid: ID!, $cid: ID!, $title: String!, $description: String, $priority: Priority!) {
            createIssue(projectID: $pid, creatorID: $cid, title: $title, description: $description, priority: $priority) { id number title }
          }`,
          variables: {
            pid: projectId,
            cid: creatorId,
            title: data.title,
            description: data.description || null,
            priority: data.priority,
          },
        }),
      });
      const json = await res.json();
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

  const priorityLabels: Record<string, string> = {
    critical: "关键",
    high: "高",
    medium: "中",
    low: "低",
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className="mr-1 h-4 w-4" />
          创建 Issue
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>创建 Issue</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="title">标题</Label>
            <Input
              id="title"
              placeholder="Issue 标题"
              {...register("title")}
            />
            {errors.title && (
              <p className="text-xs text-destructive">{errors.title.message}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="priority">优先级</Label>
            <Select
              defaultValue="medium"
              onValueChange={(v) => setValue("priority", v)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(priorityLabels).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {errors.priority && (
              <p className="text-xs text-destructive">{errors.priority.message}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="description">描述（可选）</Label>
            <Textarea
              id="description"
              placeholder="支持 Markdown 格式"
              rows={4}
              {...register("description")}
            />
          </div>

          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? "创建中..." : "创建"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
