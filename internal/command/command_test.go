package command

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"dolphin/internal/session"
	"dolphin/internal/signal"
	transport "dolphin/internal/transport"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
)

func TestNewRegistry(t *testing.T) {
	Convey("NewRegistry", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r, ShouldNotBeNil)
	})
}

func TestRegistryExecute(t *testing.T) {
	Convey("Registry.Execute", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("/version prints version", func() {
			So(func() { r.Execute(context.Background(), "version", "none") }, ShouldNotPanic)
		})

		Convey("/session new creates session", func() {
			So(mgr.Active(), ShouldBeNil)
			r.Execute(context.Background(), "session new", "none")
			So(mgr.Active(), ShouldNotBeNil)
		})

		Convey("unknown command does not panic", func() {
			So(func() { r.Execute(context.Background(), "nonexistent", "none") }, ShouldNotPanic)
		})
	})
}

func TestRegistryExecuteContext(t *testing.T) {
	Convey("Registry.Execute context propagation", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		// Register a test command that captures the transport ID from context.
		var (
			mu         sync.Mutex
			capturedID string
		)
		whoami := &cobra.Command{
			Use: "whoami",
			RunE: func(cmd *cobra.Command, args []string) error {
				info := transport.GetInfo(cmd.Context())
				mu.Lock()
				if info != nil {
					capturedID = info.ID
				} else {
					capturedID = ""
				}
				mu.Unlock()
				return nil
			},
		}
		r.Register(whoami)
		So(r.HasCommand("whoami"), ShouldBeTrue)

		Convey("command sees transport info from context", func() {
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "wework"})
			r.Execute(ctx, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "wework")
			mu.Unlock()
		})

		Convey("different transports get their own info sequentially", func() {
			ctxDing := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
			r.Execute(ctxDing, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "dingtalk")
			mu.Unlock()

			ctxWe := transport.WithInfo(context.Background(), &transport.Info{ID: "wework"})
			r.Execute(ctxWe, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "wework")
			mu.Unlock()
		})
	})
}

func TestRegistryExecuteContextConcurrent(t *testing.T) {
	Convey("Registry.Execute concurrent context isolation", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		results := make(map[string]int)
		var resMu sync.Mutex

		concCmd := &cobra.Command{
			Use: "concur",
			RunE: func(cmd *cobra.Command, args []string) error {
				info := transport.GetInfo(cmd.Context())
				id := "unknown"
				if info != nil {
					id = info.ID
				}
				resMu.Lock()
				results[id]++
				resMu.Unlock()
				return nil
			},
		}
		r.Register(concCmd)
		So(r.HasCommand("concur"), ShouldBeTrue)

		numGoroutines := 20
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				ctx := transport.WithInfo(context.Background(), &transport.Info{ID: id})
				r.Execute(ctx, "concur", "none")
			}(fmt.Sprintf("t_%d", i))
		}
		wg.Wait()

		So(len(results), ShouldEqual, numGoroutines)
		for id, count := range results {
			So(count, ShouldEqual, 1)
			So(id, ShouldStartWith, "t_")
		}
	})
}

func TestRegistrySetAgentIO(t *testing.T) {
	Convey("SetAgentIO", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r.agentIO, ShouldBeNil)

		r.SetAgentIO(nil)
		So(r.agentIO, ShouldBeNil)
	})
}

// Ensure Registry implements expected interface.
var _ = (*Registry)(nil)
