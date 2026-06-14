package workflow

import "dolphin/internal/i18n"

func init() {
	i18n.Register("workflow",
		"en", i18n.Dict{
			"workflow.started":          "[workflow] %s: starting, %d steps",
			"workflow.step_running":     "[workflow] %s: %s running...",
			"workflow.step_done":        "[workflow] %s: %s done (%s)",
			"workflow.step_failed":      "[workflow] %s: %s failed: %s",
			"workflow.step_skipped":     "[workflow] %s: %s skipped",
			"workflow.foreach_expand":   "[workflow] %s: %s expanded to %d instances",
			"workflow.instance_done":    "[workflow] %s: %s done",
			"workflow.checkpoint_pause": "[workflow] %s: checkpoint reached — review result.yaml and continue",
			"workflow.completed":        "[workflow] %s: all steps completed (%s)",
			"workflow.failed":           "[workflow] %s: failed — %s",
			"workflow.resume":           "[workflow] %s: resuming from checkpoint, %d/%d steps done",
			"workflow.cycle_error":      "workflow contains a cycle involving step %q",
			"workflow.dep_missing":      "step %q depends on unknown step %q",
			"workflow.foreach_no_schema": "foreach step %q references %s but target has no output_schema",
			"workflow.foreach_not_array": "foreach step %q: %s is not an array",
			"workflow.schema_mismatch":   "step %q output does not match schema: %s",
			"workflow.step_timeout":     "step %q timed out after %s",

			"tool.run_workflow":         "Execute a workflow YAML file",
			"tool.run_workflow_path":    "Path to the .workflow.yaml file",
			"tool.continue_workflow":    "Continue a paused workflow from checkpoint",
			"tool.continue_workflow_path": "Path to the .workflow.yaml file",
		},
		"zh", i18n.Dict{
			"workflow.started":          "[workflow] %s: 开始执行，共 %d 步",
			"workflow.step_running":     "[workflow] %s: %s 运行中...",
			"workflow.step_done":        "[workflow] %s: %s 完成 (%s)",
			"workflow.step_failed":      "[workflow] %s: %s 失败: %s",
			"workflow.step_skipped":     "[workflow] %s: %s 已跳过",
			"workflow.foreach_expand":   "[workflow] %s: %s 展开为 %d 个实例",
			"workflow.instance_done":    "[workflow] %s: %s 完成",
			"workflow.checkpoint_pause": "[workflow] %s: 检查点暂停 — 查看 result.yaml 后继续",
			"workflow.completed":        "[workflow] %s: 全部完成 (%s)",
			"workflow.failed":           "[workflow] %s: 失败 — %s",
			"workflow.resume":           "[workflow] %s: 从检查点恢复，已完成 %d/%d 步",
			"workflow.cycle_error":      "workflow 中存在循环依赖，涉及步骤 %q",
			"workflow.dep_missing":      "步骤 %q 依赖了不存在的步骤 %q",
			"workflow.foreach_no_schema": "foreach 步骤 %q 引用了 %s 但目标没有 output_schema",
			"workflow.foreach_not_array": "foreach 步骤 %q: %s 不是数组",
			"workflow.schema_mismatch":   "步骤 %q 输出与 schema 不匹配: %s",
			"workflow.step_timeout":     "步骤 %q 超时 (%s)",

			"tool.run_workflow":         "执行工作流 YAML 文件",
			"tool.run_workflow_path":    ".workflow.yaml 文件路径",
			"tool.continue_workflow":    "从检查点继续暂停的工作流",
			"tool.continue_workflow_path": ".workflow.yaml 文件路径",
		},
	)
}
