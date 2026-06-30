package agentmesh

import (
	"context"
	"sync"
	"time"

	"github.com/rs/xid"
)

// taskState is the lifecycle of an async delegation tracked by AgentMesh.
type taskState struct {
	id        string
	created   time.Time
	done      chan struct{}
	result    *DelegateResult
	err       error
	cancel    context.CancelFunc // cancels the underlying Delegate ctx
	parentSID string
	childName string
}

// taskManager tracks in-flight async delegations on the delegator side.
// It supports DelegateAsync (returns id immediately), GetResult (poll), and
// Cancel (interrupt). Phase 2: this is delegator-side tracking; the peer agent
// executes synchronously and the async-ness is local.
type taskManager struct {
	mu    sync.Mutex
	tasks map[string]*taskState
}

func newTaskManager() *taskManager {
	return &taskManager{tasks: map[string]*taskState{}}
}

// start launches a background Delegate and returns its task id immediately.
// The provided baseCtx should be the AgentMesh's background context.
func (tm *taskManager) start(baseCtx context.Context, run func(ctx context.Context) (*DelegateResult, error), parentSID, childName string) string {
	id := xid.New().String()
	ctx, cancel := context.WithCancel(baseCtx)
	st := &taskState{
		id:        id,
		created:   time.Now(),
		done:      make(chan struct{}),
		cancel:    cancel,
		parentSID: parentSID,
		childName: childName,
	}
	tm.mu.Lock()
	tm.tasks[id] = st
	tm.mu.Unlock()

	go func() {
		defer close(st.done)
		defer cancel()
		res, err := run(ctx)
		st.result = res
		st.err = err
		// GC: remove the entry after a grace period so GetResult can still
		// read it shortly after completion.
		time.AfterFunc(5*time.Minute, func() {
			tm.mu.Lock()
			delete(tm.tasks, id)
			tm.mu.Unlock()
		})
	}()
	return id
}

// get returns the current state of a task. ok=false if unknown/expired.
func (tm *taskManager) get(id string) (*taskState, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	st, ok := tm.tasks[id]
	return st, ok
}

// cancel requests cancellation of a task. It is a no-op (returns false) if the
// task is unknown or already done.
func (tm *taskManager) cancel(id string) bool {
	tm.mu.Lock()
	st, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-st.done:
		return false // already finished
	default:
		st.cancel()
		return true
	}
}

// list returns a snapshot of tracked task ids (observability).
func (tm *taskManager) list() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]string, 0, len(tm.tasks))
	for id := range tm.tasks {
		out = append(out, id)
	}
	return out
}
