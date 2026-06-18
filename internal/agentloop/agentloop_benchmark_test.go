package agentloop

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/event"
	"dolphin/internal/memory"
	"dolphin/internal/session"
	"dolphin/internal/signal"
)

// BenchmarkAgentLoopSingleTurn measures latency of a single turn through the
// full compositor pipeline (memory read → memory write).
func BenchmarkAgentLoopSingleTurn(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	eb := event.NewBus()
	mem := memory.NewFileMemory(&benchSessionStore{})

	compositor := NewCompositor(
		[]Stage{&MemoryReadStage{Memory: mem}},
		[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
		1,
	)

	q := make(chan *agentio.Turn, 1)
	a := NewAgentLoop(q, compositor, logger, eb, nil, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q <- &agentio.Turn{
			TurnID:      fmt.Sprintf("bench-%d", i),
			SessionID:   "bench-session",
			Input:       "benchmark input",
			TransportID: "bench-transport",
		}
		// Wait briefly for the turn to complete (memory read+write is fast).
		time.Sleep(5 * time.Millisecond)
	}
	b.StopTimer()
}

// BenchmarkAgentLoopMultiWorker measures throughput with multiple workers
// processing turns concurrently across different sessions.
func BenchmarkAgentLoopMultiWorker(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	eb := event.NewBus()

	mgr := session.NewManager(b.TempDir())
	aio := agentio.NewAgentIO(b.N+1, mgr, signal.NewBus(), logger, "bench")

	mem := memory.NewFileMemory(&benchSessionStore{})
	compositor := NewCompositor(
		[]Stage{&MemoryReadStage{Memory: mem}},
		[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
		1,
	)

	a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 4)

	var mu sync.Mutex
	completed := 0

	a.SetOnResult(func(r agentio.TurnResult) {
		if r.Done {
			mu.Lock()
			completed++
			mu.Unlock()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID:      fmt.Sprintf("bench-%d", i),
			SessionID:   fmt.Sprintf("bench-session-%d", i%8),
			Input:       "benchmark input",
			TransportID: "bench-transport",
		})
	}

	// Wait for all turns to complete.
	for {
		mu.Lock()
		c := completed
		mu.Unlock()
		if c >= b.N {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	b.StopTimer()
}

type benchSessionStore struct {
	sessions map[string]*session.Session
}

func (s *benchSessionStore) Get(id string) *session.Session {
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess := &session.Session{ID: id}
	if s.sessions == nil {
		s.sessions = make(map[string]*session.Session)
	}
	s.sessions[id] = sess
	return sess
}
