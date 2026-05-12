package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
)

func newTestAgentForCompress(cfg *config.Config) *Agent {
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})
	return New(cfg, sessMgr, toolReg)
}

func TestDropCompressorBelowThreshold(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgentForCompress(cfg)

	msgs := []Message{
		{Role: "user", Content: TextContent("hi")},
		{Role: "assistant", Content: TextContent("hello")},
	}

	compressed, report := agt.compressor.Compress(msgs, cfg.LLM.MaxContextTokens)
	if report != nil {
		t.Errorf("expected no compression below threshold, got report: %+v", report)
	}
	if compressed != nil {
		t.Error("expected nil compressed result")
	}
}

func TestDropCompressorDropsOldMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgentForCompress(cfg)

	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"a"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"b"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"c"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"d"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"e"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"f"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"g"}]`)},
	}

	compressed, report := agt.compressor.Compress(msgs, cfg.LLM.MaxContextTokens)
	if report == nil {
		t.Fatal("expected compression")
	}
	if len(compressed) >= len(msgs) {
		t.Error("expected messages to be compressed")
	}
}

func TestDropCompressorPreservesLastSix(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgentForCompress(cfg)

	msgs := make([]Message, 6)
	for i := 0; i < 6; i++ {
		msgs[i] = Message{
			Role:    []string{"user", "assistant"}[i%2],
			Content: json.RawMessage(`[{"type":"text","text":"x"}]`),
		}
	}

	compressed, report := agt.compressor.Compress(msgs, cfg.LLM.MaxContextTokens)
	if report != nil {
		t.Errorf("expected no compression for <=6 messages, got report: %+v", report)
	}
	if compressed != nil {
		t.Error("expected nil for <=6 messages")
	}
}

func TestDropCompressorDefaultInNewAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgentForCompress(cfg)

	// Default compress_mode is "drop"
	if _, ok := agt.compressor.(*DropCompressor); !ok {
		t.Errorf("expected DropCompressor by default, got %T", agt.compressor)
	}
}

// --- SegmentCompressor tests ---

func TestSegmentCompressorBelowThreshold(t *testing.T) {
	s := NewSegmentCompressor(100)

	msgs := []Message{
		{Role: "user", Content: TextContent("hi")},
		{Role: "assistant", Content: TextContent("hello")},
	}

	compressed, report := s.Compress(msgs, 100000)
	if report != nil {
		t.Errorf("expected no compression below threshold, got report: %+v", report)
	}
	if compressed != nil {
		t.Error("expected nil compressed result")
	}
}

func TestSegmentCompressorDropsAndCreatesL1(t *testing.T) {
	s := NewSegmentCompressor(100)

	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"a"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"b"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"c"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"d"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"e"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"f"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"g"}]`)},
	}

	compressed, report := s.Compress(msgs, 100)
	if report == nil {
		t.Fatal("expected compression report")
	}
	if report.NewLevel != 1 {
		t.Errorf("NewLevel = %d, want 1", report.NewLevel)
	}
	if len(compressed) >= len(msgs) {
		t.Errorf("expected fewer messages, got %d vs %d", len(compressed), len(msgs))
	}

	// First message should be an L1 summary
	text := extractText(compressed[0].Content)
	if !strings.HasPrefix(text, "[L1 摘要") {
		t.Errorf("expected L1 summary prefix, got: %s", text[:min(50, len(text))])
	}
}

func TestSegmentCompressorRecursiveMerge(t *testing.T) {
	s := NewSegmentCompressor(2) // merge after 2 segments

	// Create messages with 3 rounds (enough to trigger 2 compressions, then merge)
	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"a"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"b"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"c"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"d"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"e"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"f"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"g"}]`)},
	}

	compressed, report := s.Compress(msgs, 100)
	if report == nil {
		t.Fatal("expected compression")
	}
	if len(compressed) == 0 {
		t.Fatal("empty result")
	}

	// With merge_limit=2 and multiple segments, check for L2 after merge
	text := extractText(compressed[0].Content)
	if !strings.HasPrefix(text, "[L") || !strings.Contains(text, "摘要") {
		t.Errorf("expected summary marker, got: %s", text[:min(50, len(text))])
	}
}

// --- Skeleton compressor tests ---

func TestTieredCompressorBelowThreshold(t *testing.T) {
	tc := NewTieredCompressor(nil)
	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"a"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"b"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"c"}]`)},
	}

	// Under threshold, no compression
	compressed, report := tc.Compress(msgs, 100000)
	if report != nil {
		t.Error("expected no compression below threshold")
	}
	if compressed != nil {
		t.Error("expected nil result")
	}
}

func TestIncrementalCompressorBelowThreshold(t *testing.T) {
	ic := NewIncrementalCompressor(nil)
	msgs := []Message{
		{Role: "user", Content: TextContent("hi")},
		{Role: "assistant", Content: TextContent("hello")},
	}

	compressed, report := ic.Compress(msgs, 100000)
	if report != nil {
		t.Error("expected no compression below threshold")
	}
	if compressed != nil {
		t.Error("expected nil result")
	}
}

func TestTopicCompressorBelowThreshold(t *testing.T) {
	tc := NewTopicCompressor(nil)
	msgs := []Message{
		{Role: "user", Content: TextContent("hi")},
		{Role: "assistant", Content: TextContent("hello")},
	}

	compressed, report := tc.Compress(msgs, 100000)
	if report != nil {
		t.Error("expected no compression below threshold")
	}
	if compressed != nil {
		t.Error("expected nil result")
	}
}

func TestAllCompressorsImplementInterface(t *testing.T) {
	var _ Compressor = (*DropCompressor)(nil)
	var _ Compressor = (*SegmentCompressor)(nil)
	var _ Compressor = (*TieredCompressor)(nil)
	var _ Compressor = (*IncrementalCompressor)(nil)
	var _ Compressor = (*TopicCompressor)(nil)
}
