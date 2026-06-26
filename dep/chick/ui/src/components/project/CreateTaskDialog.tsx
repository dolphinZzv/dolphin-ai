import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { gql } from "@/lib/graphql";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
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

const schema = z.object({
  title: z.string().min(1, "标题不能为空").max(500, "标题不能超过 500 字"),
  description: z.string().optional(),
  priority: z.string().optional(),
});

type FormData = z.infer<typeof schema>;

interface Props {
  proposalId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

export function CreateTaskDialog({ proposalId, open, onOpenChange, onCreated }: Props) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const {
    register,
    handleSubmit,
    setValue,
    reset,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: { title: "", description: "", priority: "medium" },
  });

  const onSubmit = async (data: FormData) => {
    setSubmitting(true);
    setError("");
    try {
      const json = await gql(
        `mutation createTask($proposalId: ID!, $title: String!, $description: String, $priority: Priority) {
          createTask(proposalID: $proposalId, title: $title, description: $description, priority: $priority) { id number title state }
        }`,
        {
          proposalId,
          title: data.title,
          description: data.description || null,
          priority: data.priority || "medium",
        }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        toast.error(json.errors[0].message);
        return;
      }
      reset();
      onOpenChange(false);
      toast.success("任务创建成功");
      onCreated();
    } catch {
      setError("网络错误，请重试");
    } finally {
      setSubmitting(false);
    }
  };

  const priorityLabels: Record<string, string> = {
    critical: "关键", high: "高", medium: "中", low: "低",
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>创建任务</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="task-title">标题</Label>
            <Input id="task-title" placeholder="任务标题" {...register("title")} />
            {errors.title && <p className="text-xs text-destructive">{errors.title.message}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="task-desc">描述（可选）</Label>
            <Textarea id="task-desc" placeholder="支持 Markdown 格式" rows={3} {...register("description")} />
          </div>

          <div className="space-y-2">
            <Label>优先级</Label>
            <Select defaultValue="medium" onValueChange={(v) => setValue("priority", v)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(priorityLabels).map(([v, l]) => (
                  <SelectItem key={v} value={v}>{l}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {error && <p className="text-xs text-destructive">{error}</p>}

          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
            <Button type="submit" disabled={submitting}>{submitting ? "创建中..." : "创建"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
