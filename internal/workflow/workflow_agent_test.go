package workflow

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/tool"
)

// mockDelegator is a workflow.Delegator for testing agent step delegation.
type mockDelegator struct {
	enabled  bool
	calls    []string // recorded preferred agents
	response *DelegateResult
	err      error
}

func (m *mockDelegator) Enabled() bool { return m.enabled }

func (m *mockDelegator) Delegate(ctx context.Context, p DelegatePayload) (*DelegateResult, error) {
	m.calls = append(m.calls, p.PreferredAgent)
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &DelegateResult{Status: "completed", Content: "mock-agent-result"}, nil
}

func TestWorkflow_AgentStep_Delegates(t *testing.T) {
	Convey("a step with agent set delegates via the Delegator", t, func() {
		toolReg := tool.NewRegistry()
		mock := &mockLLMProvider{chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
			return []llm.LLMChunk{textChunk("should-not-be-used")}
		}}
		cfg := config.LoadConfigFromMap(map[string]any{"workflow.step_timeout": "30s"})
		engine := NewEngine(toolReg, mock, event.NewBus(), zap.NewNop(), nil, cfg)

		dep := &mockDelegator{enabled: true, response: &DelegateResult{Status: "completed", Content: "预言家:3 号是狼人"}}
		engine.SetDelegator(dep)

		spec := &WorkflowSpec{
			Version: "1", Name: "night",
			Steps: []StepSpec{
				{ID: "divine", Prompt: "查验 3 号身份", Agent: "seer"},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(dep.calls, ShouldResemble, []string{"seer"})
	})
}

func TestWorkflow_AgentStep_DisabledFallsBackLocally(t *testing.T) {
	Convey("when mesh is disabled, an agent step runs locally with a warning", t, func() {
		toolReg := tool.NewRegistry()
		mock := &mockLLMProvider{chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
			return []llm.LLMChunk{textChunk("本地兜底")}
		}}
		cfg := config.LoadConfigFromMap(map[string]any{"workflow.step_timeout": "30s"})
		engine := NewEngine(toolReg, mock, event.NewBus(), zap.NewNop(), nil, cfg)

		dep := &mockDelegator{enabled: false}
		engine.SetDelegator(dep)

		spec := &WorkflowSpec{
			Version: "1", Name: "night",
			Steps: []StepSpec{
				{ID: "divine", Prompt: "查验 3 号身份", Agent: "seer"},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(dep.calls, ShouldBeEmpty) // never delegated
	})
}
