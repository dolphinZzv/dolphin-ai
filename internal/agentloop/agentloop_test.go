package agentloop

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/skill"
	"dolphin/internal/testhelper"
	"dolphin/internal/types"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestCompositor(t *testing.T) {
	Convey("Compositor", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("Execute runs init then loop stages", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)

			c := NewCompositor(
				[]Stage{
					&MemoryReadStage{Memory: mem},
				},
				[]Stage{
					&MemoryWriteStage{Memory: mem, EventBus: event.NewBus()},
				},
				10,
			)

			state := &State{
				SessionID: "test-session",
				Input:     "hello",
			}

			err := c.Execute(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeTrue)
			So(len(state.Messages), ShouldBeGreaterThan, 0)
		})

		Convey("Compositor respects max rounds", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)

			rounds := 0
			c := NewCompositor(
				[]Stage{&MemoryReadStage{Memory: mem}},
				[]Stage{&incrementStage{count: &rounds}},
				3,
			)

			state := &State{
				SessionID: "test",
				Input:     "x",
			}

			err := c.Execute(context.Background(), state)
			So(err, ShouldBeNil)
			So(rounds, ShouldEqual, 3)
		})

		Convey("MemoryReadStage reads history", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			mem.Write(context.Background(), "sid", types.Message{
				Role: types.RoleUser, Content: "prev",
			})

			stage := &MemoryReadStage{Memory: mem}
			state := &State{SessionID: "sid", Input: "new"}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.History), ShouldEqual, 1)
			So(len(state.Messages), ShouldEqual, 2)
			So(state.Messages[1].Content, ShouldEqual, "new")
		})

		Convey("ContextBuilderStage injects skills", func() {
			skStore := skill.NewFileStore(t.TempDir())
			skStore.Save(context.Background(), skill.Skill{
				Name:    "helper",
				Prompt:  "you are a helper",
				Enabled: true,
			})

			stage := &ContextBuilderStage{
				BaseSystemPrompt: "base prompt",
				SkillStore:       skStore,
			}
			state := &State{}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.SystemPrompt, ShouldContainSubstring, "base prompt")
			So(state.SystemPrompt, ShouldContainSubstring, "helper")
		})

		Convey("MemoryWriteStage skips when tools were called", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			stage := &MemoryWriteStage{Memory: mem, EventBus: event.NewBus()}
			state := &State{
				SessionID:   "sid",
				Input:       "x",
				ToolsCalled: true,
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeFalse)
		})

		Convey("MemoryWriteStage writes messages", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			stage := &MemoryWriteStage{Memory: mem, EventBus: event.NewBus()}
			state := &State{
				SessionID: "sid",
				Input:     "hi",
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "hi"},
					{Role: types.RoleAssistant, Content: "hello"},
				},
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeTrue)

			msgs, _ := mem.Read(context.Background(), "sid")
			So(len(msgs), ShouldEqual, 2)
		})

		Convey("LLMStage retries on error", func() {
			eb := event.NewBus()

			provider := llm.NewProvider(llm.Config{
				Provider:   "openai",
				Model:      "gpt-4o",
				APIKey:     "invalid",
				BaseURL:    "http://127.0.0.1:1",
				MaxRetries: 1,
				MaxTokens:  10,
			}, logger)

			stage := &LLMStage{
				Provider:   provider,
				Model:      "gpt-4o",
				MaxTokens:  10,
				MaxRetries: 1,
				EventBus:   eb,
				Logger:     logger,
			}

			state := &State{
				SessionID:    "sid",
				Input:        "hi",
				SystemPrompt: "be helpful",
				Messages:     []types.Message{{Role: types.RoleUser, Content: "hi"}},
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRealLLMCompositor(t *testing.T) {
	testhelper.LoadEnv()
	if os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY") == "" {
		t.Skip("DOLPHIN_LLM_ANTHROPIC_API_KEY not set — real LLM test skipped")
	}

	Convey("Real LLM streaming", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("Provider returns first char of 123456", func() {
			provider := llm.NewProvider(llm.Config{
				Provider:   "anthropic",
				Model:      "deepseek-v4-flash",
				APIKey:     os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY"),
				BaseURL:    "https://api.deepseek.com/anthropic",
				MaxRetries: 1,
				MaxTokens:  300,
				Timeout:    60 * time.Second,
			}, logger)

			ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
				Model:     "deepseek-v4-flash",
				MaxTokens: 300,
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "123456的第一个字是什么?只回答一个字符"},
				},
			})
			So(err, ShouldBeNil)

			var content string
			var gotDone bool
			for chunk := range ch {
				So(chunk.Error, ShouldBeNil)
				content += chunk.Content
				if chunk.Done {
					gotDone = true
				}
			}
			So(gotDone, ShouldBeTrue)
			So(content, ShouldNotBeBlank)
			So(strings.TrimSpace(content), ShouldEqual, "1")
		})

		Convey("Provider respects max tokens", func() {
			provider := llm.NewProvider(llm.Config{
				Provider:   "anthropic",
				Model:      "deepseek-v4-flash",
				APIKey:     os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY"),
				BaseURL:    "https://api.deepseek.com/anthropic",
				MaxRetries: 0,
				MaxTokens:  10,
				Timeout:    60 * time.Second,
			}, logger)

			ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
				Model:     "deepseek-v4-flash",
				MaxTokens: 10,
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "写一篇长文章"},
				},
			})
			So(err, ShouldBeNil)

			var content string
			for chunk := range ch {
				So(chunk.Error, ShouldBeNil)
				content += chunk.Content
			}
			/* max_tokens=10 on thinking model may produce zero text */
			So(len(content), ShouldBeLessThanOrEqualTo, 20)
		})
	})
}

type countingMemory struct {
	inner memory.Memory
}

func (m *countingMemory) Read(ctx context.Context, sid string) ([]types.Message, error) {
	return m.inner.Read(ctx, sid)
}
func (m *countingMemory) Write(ctx context.Context, sid string, msg types.Message) error {
	return m.inner.Write(ctx, sid, msg)
}

type incrementStage struct {
	count *int
}

func (s *incrementStage) Name() string { return "increment" }
func (s *incrementStage) Process(ctx context.Context, state *State) error {
	*s.count++
	return nil
}
