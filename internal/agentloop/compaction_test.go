package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/types"
)

var errSimulated = errors.New("simulated summary failure")

// summaryProvider returns a fixed summary string for any request.
type summaryProvider struct {
	content     string
	err         error // returned before streaming starts
	chunkErr    error // delivered inside the stream as a chunk error
	emptyOutput bool  // stream completes with no content
	gotReq      llm.LLMRequest
}

func (s *summaryProvider) Name() string        { return "summary-provider" }
func (s *summaryProvider) ActiveModel() string { return "test-model" }
func (s *summaryProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}

func (s *summaryProvider) CompleteStream(_ context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	s.gotReq = req
	ch := make(chan llm.LLMChunk, 2)
	if s.err != nil {
		close(ch)
		return ch, s.err
	}
	if s.chunkErr != nil {
		ch <- llm.LLMChunk{Error: s.chunkErr}
		close(ch)
		return ch, nil
	}
	if !s.emptyOutput {
		ch <- llm.LLMChunk{Content: s.content, Done: true}
	}
	close(ch)
	return ch, nil
}

// memStore is a minimal in-memory Memory for compaction tests; it only
// needs to satisfy the Memory interface and record Replace calls.
type memStore struct {
	msgs []types.Message
}

func (m *memStore) Read(_ context.Context, _ string) ([]types.Message, error) {
	return m.msgs, nil
}

func (m *memStore) Write(_ context.Context, _ string, msg types.Message) error {
	m.msgs = append(m.msgs, msg)
	return nil
}

func (m *memStore) Replace(_ context.Context, _ string, msgs []types.Message) error {
	m.msgs = append([]types.Message{}, msgs...)
	return nil
}

func newCompactionStage(p llm.Provider, mem memory.Memory, threshold, keep int) *CompactionStage {
	return &CompactionStage{
		Provider:     p,
		Memory:       mem,
		MaxTokens:    256,
		MaxThreshold: threshold,
		KeepRounds:   keep,
		TokenRatio:   4,
		EventBus:     event.NewBus(),
	}
}

// buildMessages creates n rounds of (user, assistant) plus a trailing
// current user input. Each message's content is `fill` repeated to size
// the estimated token count.
func buildMessages(rounds int, fill string) []types.Message {
	var msgs []types.Message
	t := time.Now()
	for i := 0; i < rounds; i++ {
		msgs = append(msgs, types.Message{
			Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(fill)}, Timestamp: t,
		})
		msgs = append(msgs, types.Message{
			Role: types.RoleAssistant, Parts: []types.ContentPart{types.TextPart(fill)}, Timestamp: t,
		})
	}
	msgs = append(msgs, types.Message{
		Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("current input")}, Timestamp: t,
	})
	return msgs
}

func TestCompaction_BelowThresholdSkips(t *testing.T) {
	conveyCtx := t
	Convey("below threshold, compaction is a no-op", conveyCtx, func() {
		p := &summaryProvider{content: "SUMMARY"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100000, 2)
		msgs := buildMessages(5, "hello")
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		// Messages unchanged.
		So(len(state.Messages), ShouldEqual, len(msgs))
		So(state.Messages[0].IsSummary, ShouldBeFalse)
		// No Replace happened (store still empty / original).
		So(len(mem.msgs), ShouldEqual, 0)
	})
}

func TestCompaction_AboveThresholdCompacts(t *testing.T) {
	Convey("above threshold, old messages are summarized", t, func() {
		p := &summaryProvider{content: "key facts here"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2) // low threshold to force trigger
		// Big content so estimateTokens exceeds 100.
		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)

		// Result: [summary] + tail(keepRounds*2 + current input = 5)
		So(len(state.Messages), ShouldEqual, 1+2*2+1)
		So(state.Messages[0].IsSummary, ShouldBeTrue)
		So(state.Messages[0].Text(), ShouldContainSubstring, "key facts here")
		So(state.Messages[0].Role, ShouldEqual, types.RoleUser)
		// Tail preserved verbatim, current input last.
		So(state.Messages[len(state.Messages)-1].Text(), ShouldEqual, "current input")

		// History re-aligned to compacted list minus current input.
		So(len(state.History), ShouldEqual, len(state.Messages)-1)
		So(state.History[0].IsSummary, ShouldBeTrue)

		// Persisted via Replace (store holds compacted history).
		So(len(mem.msgs), ShouldEqual, len(state.History))
		So(mem.msgs[0].IsSummary, ShouldBeTrue)

		// Summary request carried the old conversation.
		So(p.gotReq.Messages, ShouldHaveLength, 1)
		So(p.gotReq.Messages[0].Text(), ShouldContainSubstring, "User:")
	})
}

func TestCompaction_TailDoesNotOrphanToolResult(t *testing.T) {
	Convey("split point walks back so a tool_result keeps its tool_use in tail", t, func() {
		p := &summaryProvider{content: "summary"}
		mem := &memStore{}
		big := strings.Repeat("x", 2000)
		t0 := time.Now()
		// 8 messages. With keepRounds=2, the natural split is
		// len(8) - keep*2 - 1 = 3, i.e. msgs[3] (a tool_result). Without
		// the walk-back guard, the tool_result would start the tail while
		// its tool_use (msgs[2]) stayed in old and got summarized — an
		// orphan that providers reject.
		msgs := []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(big)}, Timestamp: t0},                                                                           // 0 old
			{Role: types.RoleAssistant, Parts: []types.ContentPart{types.TextPart(big)}, Timestamp: t0},                                                                      // 1 old
			{Role: types.RoleAssistant, Parts: []types.ContentPart{types.TextPart(big)}, ToolCalls: []types.ToolCall{{ID: "c1", Name: "n", Arguments: "{}"}}, Timestamp: t0}, // 2 old (tool_use)
			{Role: types.RoleTool, Parts: []types.ContentPart{types.TextPart(big)}, ToolCallID: "c1", Timestamp: t0},                                                         // 3 natural split (tool) -> walk back to 2
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(big)}, Timestamp: t0},                                                                           // 4 tail
			{Role: types.RoleAssistant, Parts: []types.ContentPart{types.TextPart(big)}, Timestamp: t0},                                                                      // 5 tail
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(big)}, Timestamp: t0},                                                                           // 6 tail
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("current input")}, Timestamp: t0},                                                               // 7 current
		}
		s := newCompactionStage(p, mem, 100, 2)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)

		// The tool_result and its matching tool_use must both be in the
		// compacted result (tail), adjacent and ID-linked.
		toolIdx := -1
		for i, m := range state.Messages {
			if m.Role == types.RoleTool {
				toolIdx = i
				break
			}
		}
		So(toolIdx, ShouldBeGreaterThan, 0)
		So(state.Messages[toolIdx-1].Role, ShouldEqual, types.RoleAssistant)
		So(state.Messages[toolIdx-1].ToolCalls, ShouldHaveLength, 1)
		So(state.Messages[toolIdx-1].ToolCalls[0].ID, ShouldEqual, "c1")
	})
}

func TestCompaction_FoldsPriorSummary(t *testing.T) {
	Convey("an existing summary in old messages is folded into the new summary", t, func() {
		p := &summaryProvider{content: "integrated summary"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		big := strings.Repeat("x", 2000)
		t0 := time.Now()
		// A realistic post-compaction history: a leading summary from a
		// prior compaction, then a few normal turns, then new input.
		msgs := buildMessages(4, big)
		msgs[0] = types.Message{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("PRIOR")}, IsSummary: true, Timestamp: t0}
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)

		// The prior summary is included in the summarizer's prompt so its
		// content is not lost, and the new head is a fresh summary.
		So(p.gotReq.Messages[0].Text(), ShouldContainSubstring, "[Prior summary]")
		So(state.Messages[0].IsSummary, ShouldBeTrue)
		So(state.Messages[0].Text(), ShouldContainSubstring, "integrated summary")
	})
}

func TestCompaction_StreamErrorFallsBack(t *testing.T) {
	Convey("an error delivered mid-stream leaves messages unchanged", t, func() {
		p := &summaryProvider{chunkErr: errSimulated}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(state.Messages[0].IsSummary, ShouldBeFalse)
		So(len(mem.msgs), ShouldEqual, 0)
	})
}

func TestCompaction_EmptySummaryFallsBack(t *testing.T) {
	Convey("an empty summary output leaves messages unchanged", t, func() {
		p := &summaryProvider{emptyOutput: true}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(state.Messages[0].IsSummary, ShouldBeFalse)
		So(len(mem.msgs), ShouldEqual, 0)
	})
}

func TestCompaction_SummaryFailureFallsBack(t *testing.T) {
	Convey("on summary error, messages are left unchanged", t, func() {
		p := &summaryProvider{err: errSimulated}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		origLen := len(msgs)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		// Process itself does not return the error (best-effort).
		So(err, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, origLen)
		So(state.Messages[0].IsSummary, ShouldBeFalse)
		// Nothing persisted.
		So(len(mem.msgs), ShouldEqual, 0)
	})
}

func TestCompaction_TooFewMessagesSkips(t *testing.T) {
	Convey("fewer than keepRounds*2+1 messages skips compaction", t, func() {
		p := &summaryProvider{content: "summary"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 1, 6) // needs >= 13 msgs
		msgs := buildMessages(2, "x")         // 5 msgs total
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, len(msgs))
		So(state.Messages[0].IsSummary, ShouldBeFalse)
	})
}

// TestCompaction_RealInputTokensTriggers verifies that the previous turn's
// real input-token count (last_input_tokens from the provider) drives the
// threshold — not just the rune-based estimate, which misses system prompts
// and tool schemas. With short message content (rune estimate well below
// threshold) but a high last_input_tokens, compaction must still fire.
func TestCompaction_RealInputTokensTriggers(t *testing.T) {
	Convey("last_input_tokens above threshold triggers compaction", t, func() {
		p := &summaryProvider{content: "real-token summary"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 5000, 2) // threshold high vs short content

		// Short content: rune estimate ~ tens of tokens, far below 5000.
		msgs := buildMessages(5, "short")
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		// Session with a real last_input_tokens above the threshold, as if
		// the prior turn's full request (system prompt + tools + history)
		// was large even though message content here is small.
		mgr := session.NewManager(t.TempDir())
		sess := mgr.Create(context.Background())
		sess.Set("last_input_tokens", 6000)
		s.SessionMgr = mgr
		state.SessionID = sess.ID

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(state.Messages[0].IsSummary, ShouldBeTrue)
	})

	Convey("without SessionMgr the rune estimate still drives the threshold", t, func() {
		p := &summaryProvider{content: "rune summary"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2) // low threshold
		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}
		// SessionMgr intentionally nil.

		err := s.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(state.Messages[0].IsSummary, ShouldBeTrue)
	})
}

// gatingSummaryProvider returns a stream that blocks on `release` before
// delivering the summary content. Used to test Pause/Resume mid-compaction.
type gatingSummaryProvider struct {
	release chan struct{}
	content string
}

func (g *gatingSummaryProvider) Name() string        { return "gating" }
func (g *gatingSummaryProvider) ActiveModel() string { return "test-model" }
func (g *gatingSummaryProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}
func (g *gatingSummaryProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk, 1)
	go func() {
		<-g.release
		ch <- llm.LLMChunk{Content: g.content, Done: true}
		close(ch)
	}()
	return ch, nil
}

func TestCompaction_PauseResumesMidSummary(t *testing.T) {
	Convey("Pause blocks compaction and Resume continues it", t, func() {
		sb := signal.NewBus()
		release := make(chan struct{})
		p := &gatingSummaryProvider{release: release, content: "resumed summary"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		s.SignalBus = sb

		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		state := &State{SessionID: "s1", Messages: msgs, History: msgs[:len(msgs)-1]}

		done := make(chan error, 1)
		go func() { done <- s.Process(context.Background(), state) }()

		// Give the stage a moment to subscribe and enter the summarize
		// stream select, then send Pause. The turn should block (not done).
		time.Sleep(50 * time.Millisecond)
		sb.Send("s1", signal.Pause)

		select {
		case <-done:
			t.Fatal("compaction should be paused, not finished")
		case <-time.After(80 * time.Millisecond):
		}

		// Resume — the paused summarize unblocks and waits on the stream
		// again. Release the provider so it delivers the summary.
		sb.Send("s1", signal.Resume)
		close(release)

		select {
		case err := <-done:
			So(err, ShouldBeNil)
			So(state.Messages[0].IsSummary, ShouldBeTrue)
			So(state.Messages[0].Text(), ShouldContainSubstring, "resumed summary")
		case <-time.After(2 * time.Second):
			t.Fatal("compaction did not complete after Resume")
		}
	})

	Convey("Interrupt during compaction aborts (best-effort fallback)", t, func() {
		sb := signal.NewBus()
		release := make(chan struct{})
		p := &gatingSummaryProvider{release: release, content: "should not appear"}
		mem := &memStore{}
		s := newCompactionStage(p, mem, 100, 2)
		s.SignalBus = sb

		big := strings.Repeat("x", 2000)
		msgs := buildMessages(5, big)
		orig := append([]types.Message{}, msgs...)
		state := &State{SessionID: "s2", Messages: msgs, History: msgs[:len(msgs)-1]}

		done := make(chan error, 1)
		go func() { done <- s.Process(context.Background(), state) }()

		time.Sleep(50 * time.Millisecond)
		sb.Send("s2", signal.Interrupt)

		select {
		case err := <-done:
			// Compaction is best-effort: Process returns nil and falls back
			// to the un-compacted messages.
			So(err, ShouldBeNil)
			So(state.Messages[0].IsSummary, ShouldBeFalse)
			So(len(state.Messages), ShouldEqual, len(orig))
		case <-time.After(2 * time.Second):
			t.Fatal("compaction did not abort after Interrupt")
		}
		close(release)
	})
}
