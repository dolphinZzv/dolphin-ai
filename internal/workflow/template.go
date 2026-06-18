package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

var varPattern = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*|\[\*\]\.[a-zA-Z_][a-zA-Z0-9_]*|\[\d+\])*)`)

// renderTemplate compiles and renders a prompt string with $step.field syntax against the given data.
func renderPrompt(prompt string, data map[string]any) (string, error) {
	goTmpl, err := compile(prompt)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := goTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template render: %w", err)
	}
	return buf.String(), nil
}

// compile converts $step.field syntax into a Go text/template.
func compile(prompt string) (*template.Template, error) {
	compiled := varPattern.ReplaceAllStringFunc(prompt, func(match string) string {
		// Handle $audit[*].result → range over all instances of audit
		if strings.Contains(match, "[*]") {
			parts := strings.SplitN(match, "[*]", 2)
			stepID := strings.TrimPrefix(parts[0], "$")
			field := strings.TrimPrefix(parts[1], ".")
			return fmt.Sprintf(`{{range $i, $_ := index . "%s"}}{{if $i}}, {{end}}{{index $_ "%s"}}{{end}}`, stepID, field)
		}

		// Handle $audit[0].result
		if idx := strings.IndexByte(match, '['); idx >= 0 {
			closeIdx := strings.IndexByte(match[idx:], ']')
			stepID := strings.TrimPrefix(match[:idx], "$")
			indexStr := match[idx+1 : idx+closeIdx]
			rest := match[idx+closeIdx+1:]
			if rest != "" && rest[0] == '.' {
				field := rest[1:]
				return fmt.Sprintf(`{{index (index . "%s" %s) "%s"}}`, stepID, indexStr, field)
			}
			return fmt.Sprintf(`{{index . "%s" %s}}`, stepID, indexStr)
		}

		// Handle $step.field
		if idx := strings.IndexByte(match, '.'); idx >= 0 {
			stepID := strings.TrimPrefix(match[:idx], "$")
			field := match[idx+1:]
			return fmt.Sprintf(`{{index . "%s" "%s"}}`, stepID, field)
		}

		// Plain $step
		return fmt.Sprintf(`{{index . "%s"}}`, strings.TrimPrefix(match, "$"))
	})

	return template.New("prompt").Parse(compiled)
}

// resolveResult traverses accumulated results to resolve a step.field reference.
func resolveResult(results map[string]*StepResult, stepID, field string) (any, error) {
	sr, ok := results[stepID]
	if !ok {
		return nil, fmt.Errorf("workflow: unknown step %q", stepID)
	}
	if sr.Result == nil {
		return nil, fmt.Errorf("workflow: step %q has no result", stepID)
	}

	if r, ok := sr.Result.(map[string]any); ok {
		v, ok := r[field]
		if !ok {
			return nil, fmt.Errorf("workflow: step %q result has no field %q", stepID, field)
		}
		return v, nil
	}
	return nil, fmt.Errorf("workflow: step %q result is not a map", stepID)
}

// collectField gathers a field from all foreach instances of a step.
func collectField(results map[string]*StepResult, stepID, field string) ([]any, error) {
	sr, ok := results[stepID]
	if !ok {
		return nil, fmt.Errorf("workflow: unknown step %q", stepID)
	}

	var out []any
	for _, inst := range sr.Instances {
		if inst.Status != StatusDone {
			continue
		}
		if inst.Result == nil {
			continue
		}
		m, ok := inst.Result.(map[string]any)
		if !ok {
			out = append(out, inst.Result)
			continue
		}
		v, ok := m[field]
		if !ok {
			out = append(out, inst.Result)
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// buildTemplateData constructs the data map for template rendering from run state.
func buildTemplateData(rs *runState, stepID string) map[string]any {
	data := make(map[string]any)

	for _, sr := range rs.results {
		if sr.Status == StatusDone {
			// Branches test different predicates (multi-instance vs single
			// result vs single instance), not equality on one value.
			if len(sr.Instances) > 1 { //nolint:gocritic // ifElseChain
				// For foreach steps (multiple instances), expose a list of instance results.
				instList := make([]map[string]any, 0, len(sr.Instances))
				for _, inst := range sr.Instances {
					if inst.Status == StatusDone {
						instList = append(instList, map[string]any{
							"key":    inst.Key,
							"result": inst.Result,
						})
					}
				}
				data[sr.ID] = instList
			} else if sr.Result != nil {
				data[sr.ID] = sr.Result
			} else if len(sr.Instances) == 1 && sr.Instances[0].Result != nil {
				data[sr.ID] = sr.Instances[0].Result
			} else {
				data[sr.ID] = sr.Error
			}
		}
	}
	return data
}
