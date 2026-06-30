package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

const maxToolRounds = 5

// executeStep runs a single workflow step instance via LLM call with bounded tool loop.
func (e *Engine) executeStep(ctx context.Context, inst stepInstance) *InstanceResult {
	start := time.Now()

	timeout := e.config.GetDuration("workflow.step_timeout")
	if inst.Timeout != "" {
		if d, err := time.ParseDuration(inst.Timeout); err == nil {
			timeout = d
		}
	}
	// timeout <= 0 means no per-step deadline. Each step runs until the
	// overall workflow context expires or the step completes naturally.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	messages := []types.Message{
		{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(inst.Prompt)}},
	}

	tools, _ := e.toolReg.List(ctx)
	maxTokens := inst.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	var allContent strings.Builder

	for round := 0; round < maxToolRounds; round++ {
		ch, err := e.llmProvider.CompleteStream(ctx, llm.LLMRequest{
			Model:     inst.Model,
			Messages:  messages,
			MaxTokens: maxTokens,
			Tools:     tools,
			Stream:    true,
		})
		if err != nil {
			e.logger.Warn("workflow step LLM error",
				zap.String("step_id", inst.StepID),
				zap.String("key", inst.Key),
				zap.Error(err),
			)
			return &InstanceResult{
				Key:    inst.Key,
				Status: StatusFailed,
				Error:  err.Error(),
			}
		}

		var content strings.Builder
		var toolCalls []types.ToolCall

		for chunk := range ch {
			if chunk.Error != nil {
				return &InstanceResult{
					Key:    inst.Key,
					Status: StatusFailed,
					Error:  chunk.Error.Error(),
				}
			}
			content.WriteString(chunk.Content)
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}

		assistantContent := content.String()
		allContent.WriteString(assistantContent)

		messages = append(messages, types.Message{
			Role:      types.RoleAssistant,
			Parts:     []types.ContentPart{types.TextPart(assistantContent)},
			ToolCalls: toolCalls,
		})

		if len(toolCalls) == 0 {
			break
		}

		// Execute tool calls.
		for _, tc := range toolCalls {
			result, err := e.toolReg.Execute(ctx, tc)
			toolMsg := types.Message{
				Role:       types.RoleTool,
				ToolCallID: tc.ID,
			}
			if err != nil {
				toolMsg.Parts = []types.ContentPart{types.TextPart(err.Error())}
				toolMsg.IsError = true
			} else if result != nil {
				toolMsg.Parts = []types.ContentPart{types.TextPart(result.Content)}
				toolMsg.IsError = result.IsError
			}
			messages = append(messages, toolMsg)
		}
	}

	result := parseOutput(allContent.String(), inst.Spec.OutputSchema)

	return &InstanceResult{
		Key:      inst.Key,
		Status:   StatusDone,
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Result:   result,
	}
}

// parseOutput attempts to parse the LLM output against the output_schema.
func parseOutput(raw string, schema map[string]any) any {
	if schema == nil {
		return raw
	}

	// Extract JSON from the raw text (LLM may wrap it in markdown fences).
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return raw
	}

	var parsed any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return raw
	}

	// Validate types against schema.
	if resultMap, ok := parsed.(map[string]any); ok {
		for key, expectedType := range schema {
			val, exists := resultMap[key]
			if !exists {
				continue // field optional
			}
			if !matchSchema(val, expectedType) {
				return raw // type mismatch, return raw text
			}
		}
	}
	return parsed
}

func extractJSON(s string) string {
	// Try to find JSON object between ```json fences.
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}

	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		return s
	}
	return ""
}

func matchSchema(val any, expectedType any) bool {
	switch et := expectedType.(type) {
	case string:
		switch et {
		case "string":
			_, ok := val.(string)
			return ok
		case "number":
			switch val.(type) {
			case float64, int, int64:
				return true
			}
			return false
		case "boolean":
			_, ok := val.(bool)
			return ok
		}
	case []any:
		// Expected type is an array literal like [string]
		if len(et) == 1 {
			if typeStr, ok := et[0].(string); ok && typeStr == "string" {
				_, ok := val.([]any)
				return ok
			}
		}
	}
	return true // unknown schema type → accept
}
