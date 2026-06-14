package workflow

import (
	"fmt"
	"strings"
)

// expandForeach resolves a foreach expression and creates step instances.
func (e *Engine) expandForeach(step StepSpec, rs *runState) ([]stepInstance, error) {
	if step.ForEach == "" {
		tplData := buildTemplateData(rs, step.ID)
		prompt, err := renderPrompt(step.Prompt, tplData)
		if err != nil {
			return nil, fmt.Errorf("workflow: step %q template error: %w", step.ID, err)
		}
		return []stepInstance{{
			StepID:    step.ID,
			Key:       step.ID,
			Prompt:    prompt,
			Timeout:   step.Timeout,
			MaxTokens: step.MaxTokens,
			Spec:      step,
		}}, nil
	}

	expr := strings.TrimPrefix(step.ForEach, "$")
	dotIdx := strings.IndexByte(expr, '.')
	if dotIdx < 0 {
		return nil, fmt.Errorf("workflow: foreach step %q has invalid expression %q", step.ID, step.ForEach)
	}
	refStepID := expr[:dotIdx]
	field := expr[dotIdx+1:]

	sr, ok := rs.results[refStepID]
	if !ok {
		return nil, fmt.Errorf("workflow: foreach step %q references unknown step %q", step.ID, refStepID)
	}
	if sr.Status != StatusDone {
		return nil, fmt.Errorf("workflow: foreach step %q references incomplete step %q", step.ID, refStepID)
	}
	if sr.Result == nil {
		return nil, fmt.Errorf("workflow: foreach step %q references step %q with no result", step.ID, refStepID)
	}

	resultMap, ok := sr.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("workflow: foreach step %q: step %q result is not a map", step.ID, refStepID)
	}

	arr, ok := resultMap[field].([]any)
	if !ok {
		return nil, fmt.Errorf("workflow: foreach step %q: %s is not an array", step.ID, step.ForEach)
	}

	instances := make([]stepInstance, 0, len(arr))
	for i, elem := range arr {
		key := fmt.Sprintf("%s[%d]", step.ID, i)
		if s, ok := elem.(string); ok {
			key = fmt.Sprintf("%s[%d-%s]", step.ID, i, truncateKey(s, 36))
		}

		tplData := buildTemplateData(rs, step.ID)
		tplData["each"] = elem
		// Also expose as a flat map for simpler templates
		if m, ok := elem.(map[string]any); ok {
			for k, v := range m {
				tplData["each_"+k] = v
			}
		}

		prompt, err := renderPrompt(step.Prompt, tplData)
		if err != nil {
			return nil, fmt.Errorf("workflow: foreach step %q template error: %w", step.ID, err)
		}

		instances = append(instances, stepInstance{
			StepID:    step.ID,
			Key:       key,
			Prompt:    prompt,
			Timeout:   step.Timeout,
			MaxTokens: step.MaxTokens,
			Spec:      step,
			Each:      elem,
		})
	}

	return instances, nil
}

func truncateKey(s string, n int) string {
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}
