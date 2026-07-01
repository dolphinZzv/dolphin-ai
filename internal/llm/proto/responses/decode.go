package responses

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/types"
)

// pendingToolCall accumulates a function call from streaming deltas.
type pendingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

// chunkDecoder implements proto.ChunkDecoder for the Responses API SSE stream.
type chunkDecoder struct {
	sse              *proto.SSEReader
	pendingToolCalls map[int]*pendingToolCall // output_index -> accumulating call

	inputTokens  int
	outputTokens int
	totalTokens  int
}

// NewChunkDecoder returns a proto.ChunkDecoder for Responses API SSE streams.
func NewChunkDecoder(r io.Reader) proto.ChunkDecoder {
	return &chunkDecoder{
		sse:              proto.NewSSEReader(r),
		pendingToolCalls: make(map[int]*pendingToolCall),
	}
}

// Decode reads the next SSE event and returns an LLMChunk.
func (d *chunkDecoder) Decode() (llm.LLMChunk, error) {
	for {
		raw, done, err := d.sse.Next()
		if err != nil {
			return llm.LLMChunk{}, err
		}
		if done {
			return d.doneChunk()
		}

		chunk, emit := d.handleEvent(raw)
		if emit {
			return chunk, nil
		}
	}
}

// handleEvent dispatches on event type and returns the chunk to emit (if any).
// The emit flag is false for internal/bookkeeping events.
func (d *chunkDecoder) handleEvent(raw []byte) (llm.LLMChunk, bool) {
	var e sseEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return llm.LLMChunk{}, false
	}

	switch e.Type {
	case EventResponseOutputItemAdded:
		return d.handleOutputItemAdded(raw)

	case EventResponseOutputTextDelta:
		return d.handleTextDelta(raw)

	case EventResponseFunctionCallArgumentsDelta:
		return d.handleFuncCallArgsDelta(raw)

	case EventResponseFunctionCallArgumentsDone:
		return d.handleFuncCallArgsDone(raw)

	case EventResponseCompleted:
		chunk, _ := d.handleCompleted(raw)
		return chunk, true

	case EventError:
		return d.handleError(raw)
	}

	// internal events (created, in_progress, output_text.done, etc.)
	return llm.LLMChunk{}, false
}

// handleOutputItemAdded extracts function call metadata.
func (d *chunkDecoder) handleOutputItemAdded(raw []byte) (llm.LLMChunk, bool) {
	var ev outputItemAdded
	if err := json.Unmarshal(raw, &ev); err != nil {
		return llm.LLMChunk{}, false
	}
	if ev.Item.Type == "function_call" {
		d.pendingToolCalls[ev.OutputIdx] = &pendingToolCall{
			id:   ev.Item.CallID,
			name: ev.Item.Name,
		}
	}
	return llm.LLMChunk{}, false
}

// handleTextDelta emits content from response.output_text.delta events.
func (d *chunkDecoder) handleTextDelta(raw []byte) (llm.LLMChunk, bool) {
	var ev outputTextDelta
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Delta == "" {
		return llm.LLMChunk{}, false
	}
	return llm.LLMChunk{Content: ev.Delta}, true
}

// handleFuncCallArgsDelta accumulates argument fragments.
func (d *chunkDecoder) handleFuncCallArgsDelta(raw []byte) (llm.LLMChunk, bool) {
	var ev funcCallArgsDelta
	if err := json.Unmarshal(raw, &ev); err != nil {
		return llm.LLMChunk{}, false
	}
	tc, ok := d.pendingToolCalls[ev.OutputIdx]
	if !ok {
		tc = &pendingToolCall{}
		d.pendingToolCalls[ev.OutputIdx] = tc
	}
	tc.arguments.WriteString(ev.Delta)
	return llm.LLMChunk{}, false
}

// handleFuncCallArgsDone flushes a completed function call.
func (d *chunkDecoder) handleFuncCallArgsDone(raw []byte) (llm.LLMChunk, bool) {
	var ev funcCallArgsDone
	if err := json.Unmarshal(raw, &ev); err != nil {
		return llm.LLMChunk{}, false
	}
	tc, ok := d.pendingToolCalls[ev.OutputIdx]
	if !ok {
		return llm.LLMChunk{}, false
	}
	delete(d.pendingToolCalls, ev.OutputIdx)

	args := tc.arguments.String()
	if ev.Arguments != "" {
		args = ev.Arguments
	}
	return llm.LLMChunk{
		ToolCalls: []types.ToolCall{{ID: tc.id, Name: tc.name, Arguments: args}},
	}, true
}

// handleCompleted processes the response.completed event, returning the
// terminal chunk with usage statistics.
func (d *chunkDecoder) handleCompleted(raw []byte) (llm.LLMChunk, bool) {
	// Flush any remaining pending tool calls first.
	if len(d.pendingToolCalls) > 0 {
		var calls []types.ToolCall
		for _, tc := range d.pendingToolCalls {
			calls = append(calls, types.ToolCall{
				ID:        tc.id,
				Name:      tc.name,
				Arguments: tc.arguments.String(),
			})
		}
		d.pendingToolCalls = nil
		// Emit remaining tool calls, let the next call return done.
		return llm.LLMChunk{ToolCalls: calls}, true
	}

	var ev responseCompleted
	if err := json.Unmarshal(raw, &ev); err == nil && ev.Response.Usage != nil {
		u := ev.Response.Usage
		d.inputTokens = u.InputTokens
		d.outputTokens = u.OutputTokens
		d.totalTokens = u.TotalTokens
	}
	chunk, _ := d.doneChunk()
	return chunk, true
}

// handleError processes error SSE events.
func (d *chunkDecoder) handleError(raw []byte) (llm.LLMChunk, bool) {
	var eb ErrorBody
	msg := "responses: stream error"
	if err := json.Unmarshal(raw, &eb); err == nil && eb.Error.Message != "" {
		msg = eb.Error.Message
	}
	return llm.LLMChunk{Error: fmt.Errorf("%s", msg)}, true
}

// doneChunk returns the terminal chunk with usage statistics.
func (d *chunkDecoder) doneChunk() (llm.LLMChunk, error) {
	return llm.LLMChunk{
		Done:         true,
		InputTokens:  d.inputTokens,
		OutputTokens: d.outputTokens,
		TotalTokens:  d.totalTokens,
	}, nil
}

// DecodeComplete parses a non-streaming Responses API response body.
func DecodeComplete(raw []byte) (llm.LLMChunk, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return llm.LLMChunk{}, err
	}

	var content strings.Builder
	var toolCalls []types.ToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					content.WriteString(part.Text)
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, types.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}

	chunk := llm.LLMChunk{
		Content:   content.String(),
		ToolCalls: toolCalls,
		Done:      resp.Status == "completed",
	}
	if resp.Usage != nil {
		chunk.InputTokens = resp.Usage.InputTokens
		chunk.OutputTokens = resp.Usage.OutputTokens
		chunk.TotalTokens = resp.Usage.TotalTokens
	}
	return chunk, nil
}
