package compressor

import (
	"context"
	"strings"
	"time"

	"dolphin/internal/agent/provider"

	"go.uber.org/zap"
)

// TopicCompressor implements strategy D: topic-aware segmentation.
// Messages are grouped by topic boundaries (detected via heuristics) and each
// completed topic group is independently summarized via an LLM call. The current
// (incomplete) topic stays raw.
type TopicCompressor struct {
	provider provider.Provider
	timeout  time.Duration
}

// NewTopicCompressor creates a TopicCompressor with an LLM provider.
func NewTopicCompressor(provider provider.Provider, timeout time.Duration) *TopicCompressor {
	return &TopicCompressor{provider: provider, timeout: timeout}
}

// topicGroup holds messages belonging to one topic.
type topicGroup struct {
	messages   []provider.Message
	userTurns  int
	totalChars int
}

func (tc *TopicCompressor) Compress(messages []provider.Message, maxTokens int) ([]provider.Message, *CompressReport) {
	pre := compressPreamble(messages, maxTokens)
	if !pre.CanDrop {
		return nil, nil
	}
	keepStart := pre.KeepStart

	groups := tc.partitionTopics(messages[:keepStart])

	if len(groups) <= 1 {
		// Not enough topics to compress, fall back to single summary
		if len(groups) == 1 && len(groups[0].messages) > 0 {
			summary := tc.summarizeTopic(groups[0].messages)
			var sb strings.Builder
			sb.WriteString("[L1 摘要, 覆盖 ")
			sb.WriteString(itoa(groups[0].userTurns))
			sb.WriteString(" 组] ")
			sb.WriteString(summary)
			result := []provider.Message{{Role: "user", Content: provider.TextContent(sb.String())}}
			result = append(result, messages[keepStart:]...)
			tokensSaved := 0
			for _, m := range groups[0].messages {
				tokensSaved += provider.EstimateTokens(string(m.Content))
			}
			tokensSaved -= provider.EstimateTokens(summary)
			return result, &CompressReport{
				DroppedCount: len(groups[0].messages),
				TokensSaved:  max(tokensSaved, 0),
				NewLevel:     1,
			}
		}
		return nil, nil
	}

	var result []provider.Message
	tokensSaved := 0
	droppedCount := 0

	// Summarize all but the last topic group (last = current topic, kept raw-ish
	// but if we're compressing, summarize all completed topics)
	for i := 0; i < len(groups); i++ {
		g := groups[i]
		summary := tc.summarizeTopic(g.messages)
		var sb strings.Builder
		sb.WriteString("[L1 摘要, topic ")
		sb.WriteString(itoa(i + 1))
		sb.WriteString(", 覆盖 ")
		sb.WriteString(itoa(g.userTurns))
		sb.WriteString(" 组] ")
		sb.WriteString(summary)
		result = append(result, provider.Message{Role: "user", Content: provider.TextContent(sb.String())})
		droppedCount += len(g.messages)
		for _, m := range g.messages {
			tokensSaved += provider.EstimateTokens(string(m.Content))
		}
		tokensSaved -= provider.EstimateTokens(summary)
	}

	result = append(result, messages[keepStart:]...)

	if droppedCount == 0 {
		return nil, nil
	}

	return result, &CompressReport{
		DroppedCount: droppedCount,
		TokensSaved:  max(tokensSaved, 0),
		NewLevel:     1,
	}
}

// partitionTopics splits messages into topic groups using heuristics.
// A new topic starts when a user message is significantly longer than the
// running average (2x), suggesting a new detailed request.
func (tc *TopicCompressor) partitionTopics(messages []provider.Message) []topicGroup {
	if len(messages) == 0 {
		return nil
	}

	// Collect user message boundaries (each user message starts a potential topic)
	type boundary struct {
		idx    int
		msgLen int
	}
	var boundaries []boundary
	for i, m := range messages {
		if m.Role == "user" {
			boundaries = append(boundaries, boundary{idx: i, msgLen: len(string(m.Content))})
		}
	}
	if len(boundaries) == 0 {
		return []topicGroup{{messages: messages, userTurns: 0}}
	}

	// Compute average user message length
	total := 0
	for _, b := range boundaries {
		total += b.msgLen
	}
	avg := total / len(boundaries)

	// Determine topic breakpoints: a new topic starts when a user message is
	// >2x the average length, or when it follows significant tool usage.
	breakpoints := map[int]bool{0: true} // first message always starts a topic
	for i := 1; i < len(boundaries); i++ {
		if boundaries[i].msgLen > avg*2 && boundaries[i].msgLen > 200 {
			breakpoints[boundaries[i].idx] = true
		}
	}

	// Group messages into topics
	var groups []topicGroup
	start := 0
	for i := 0; i < len(messages); i++ {
		if breakpoints[i] && i > start {
			group := tc.buildGroup(messages[start:i])
			if len(group.messages) > 0 {
				groups = append(groups, group)
			}
			start = i
		}
	}
	// Last group
	group := tc.buildGroup(messages[start:])
	if len(group.messages) > 0 {
		groups = append(groups, group)
	}

	return groups
}

func (tc *TopicCompressor) buildGroup(messages []provider.Message) topicGroup {
	g := topicGroup{messages: messages}
	for _, m := range messages {
		if m.Role == "user" {
			g.userTurns++
		}
		g.totalChars += len(string(m.Content))
	}
	return g
}

// summarizeTopic calls the LLM to summarize a single topic group.
func (tc *TopicCompressor) summarizeTopic(messages []provider.Message) string {
	texts := make([]string, 0, len(messages))
	for _, m := range messages {
		txt := provider.ExtractText(m.Content)
		if txt != "" {
			texts = append(texts, m.Role+": "+txt)
		}
	}
	if len(texts) == 0 {
		return ""
	}

	if tc.provider == nil {
		return strings.Join(texts, " | ")
	}

	systemPrompt := "你是一个对话摘要助手。请用1-2句话简要摘要以下对话片段，保留关键决策和结论。只输出摘要文本，不要加前缀或标记。"
	userContent := "请摘要以下对话片段：\n" + strings.Join(texts, "\n")

	timeout := tc.timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := tc.provider.Complete(ctx, provider.ProviderRequest{
		Messages:  []provider.Message{{Role: "user", Content: provider.TextContent(userContent)}},
		System:    systemPrompt,
		MaxTokens: 300,
	})
	if err != nil {
		zap.S().Debugw("topic compressor: LLM summary failed, using concatenation", "error", err)
		return strings.Join(texts, " | ")
	}

	summary := provider.ExtractText(resp.Content)
	if summary == "" {
		return strings.Join(texts, " | ")
	}
	return summary
}
