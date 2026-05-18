package compressor

import (
	"context"
	"strings"
	"sync"
	"time"

	"dolphin/internal/agent/provider"

	"go.uber.org/zap"
)

// IncrementalCompressor implements strategy C: a single running summary that is
// incrementally updated every updateInterval turns by merging new messages into
// the existing summary via an LLM call.
type IncrementalCompressor struct {
	provider       provider.Provider
	updateInterval int // merge new turns into summary every N turns (default 5)

	mu               sync.Mutex
	runningSummary   string
	turnsSinceUpdate int
	coveredCount     int // how many message groups the running summary covers
}

// NewIncrementalCompressor creates an IncrementalCompressor with an LLM provider.
func NewIncrementalCompressor(provider provider.Provider) *IncrementalCompressor {
	return &IncrementalCompressor{provider: provider, updateInterval: 5}
}

func (ic *IncrementalCompressor) Compress(messages []provider.Message, maxTokens int) ([]provider.Message, *CompressReport) {
	pre := compressPreamble(messages, maxTokens)
	if !pre.CanDrop {
		return nil, nil
	}
	keepStart := pre.KeepStart

	ic.mu.Lock()
	ic.turnsSinceUpdate++

	// Count new user turns in the prefix that will be compressed
	newTurns := 0
	for j := 0; j < keepStart; j++ {
		if messages[j].Role == "user" {
			newTurns++
		}
	}

	// If enough turns have accumulated, merge them into the running summary
	if ic.turnsSinceUpdate >= ic.updateInterval && ic.provider != nil {
		newSummary, covered := ic.mergeIntoSummary(messages[:keepStart], newTurns)
		if newSummary != "" {
			ic.runningSummary = newSummary
			ic.coveredCount += covered
			ic.turnsSinceUpdate = 0
		}
	}
	summary := ic.runningSummary
	covered := ic.coveredCount
	ic.mu.Unlock()

	var result []provider.Message
	tokensSaved := 0
	droppedCount := keepStart

	// Build result: running summary + recent raw messages
	if summary != "" {
		var sb strings.Builder
		sb.WriteString("[L1 摘要, 覆盖 ")
		sb.WriteString(itoa(covered))
		sb.WriteString(" 组] ")
		sb.WriteString(summary)
		result = append(result, provider.Message{Role: "user", Content: provider.TextContent(sb.String())})
	} else {
		// Fallback: concatenate old messages as plain text
		var parts []string
		for _, m := range messages[:keepStart] {
			txt := provider.ExtractText(m.Content)
			if txt != "" {
				parts = append(parts, m.Role+": "+txt)
			}
		}
		if len(parts) > 0 {
			result = append(result, provider.Message{Role: "user", Content: provider.TextContent("[L1 摘要] " + strings.Join(parts, " | "))})
		}
	}

	result = append(result, messages[keepStart:]...)

	for _, m := range messages[:keepStart] {
		tokensSaved += provider.EstimateTokens(string(m.Content))
	}
	if summary != "" {
		tokensSaved -= provider.EstimateTokens(summary)
	}

	if droppedCount == 0 {
		return nil, nil
	}

	return result, &CompressReport{
		DroppedCount: droppedCount,
		TokensSaved:  max(tokensSaved, 0),
		NewLevel:     1,
	}
}

// mergeIntoSummary calls the LLM to merge new messages into the existing summary.
func (ic *IncrementalCompressor) mergeIntoSummary(messages []provider.Message, newTurns int) (string, int) {
	texts := make([]string, 0, len(messages))
	for _, m := range messages {
		txt := provider.ExtractText(m.Content)
		if txt != "" {
			texts = append(texts, m.Role+": "+txt)
		}
	}
	if len(texts) == 0 {
		return "", 0
	}

	systemPrompt := "你是一个对话摘要助手。请将新对话内容合并到已有摘要中，输出更新后的摘要。用1-3句话保留关键信息。只输出摘要文本，不要加前缀或标记。"

	userContent := "已有摘要：" + ic.runningSummary + "\n\n新对话内容：\n" + strings.Join(texts, "\n") + "\n\n请输出合并后的摘要："

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := ic.provider.Complete(ctx, provider.ProviderRequest{
		Messages:  []provider.Message{{Role: "user", Content: provider.TextContent(userContent)}},
		System:    systemPrompt,
		MaxTokens: 400,
	})
	if err != nil {
		zap.S().Debugw("incremental compressor: LLM merge failed, using concatenation", "error", err)
		return "", 0
	}

	summary := provider.ExtractText(resp.Content)
	if summary == "" {
		return "", 0
	}
	return summary, newTurns
}
