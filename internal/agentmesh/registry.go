package agentmesh

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RemoteAgent is a statically-configured remote peer.
type RemoteAgent struct {
	Name         string   `yaml:"name" json:"name"`
	Addr         string   `yaml:"addr" json:"addr"` // host:port
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
	Model        string   `yaml:"model" json:"model"`
}

// LocalAgent is a statically-configured local (same-host) peer.
type LocalAgent = RemoteAgent // alias; local vs remote is decided by addr reachability

// Registry holds known agent cards. It is concurrency-safe.
//
// Cards are keyed by name. Upsert applies the conflict tie-breaker from the
// design doc: same name + same addr updates timestamp; same name + different
// addr resolves by (proto, load, version, last-seen, addr).
type Registry struct {
	mu     sync.RWMutex
	cards  map[string]*AgentCard // name → card (canonical)
	logger *zap.Logger
}

// NewRegistry builds a registry preloaded with static config.
func NewRegistry(local, remote []RemoteAgent, logger *zap.Logger) *Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &Registry{
		cards:  make(map[string]*AgentCard),
		logger: logger,
	}
	for _, a := range local {
		r.Upsert(AgentCard{
			Name:         a.Name,
			Addr:         a.Addr,
			Capabilities: a.Capabilities,
			Model:        a.Model,
			Status:       AgentRunning,
			MaxLoad:      5,
			ProtoVersion: 1,
		})
	}
	for _, a := range remote {
		r.Upsert(AgentCard{
			Name:         a.Name,
			Addr:         a.Addr,
			Capabilities: a.Capabilities,
			Model:        a.Model,
			Status:       AgentRunning,
			MaxLoad:      5,
			ProtoVersion: 1,
		})
	}
	return r
}

// Upsert inserts or updates a card, applying the conflict tie-breaker.
// Returns the canonical card currently registered under that name.
func (r *Registry) Upsert(card AgentCard) AgentCard {
	if card.MaxLoad <= 0 {
		card.MaxLoad = 5
	}
	if card.ProtoVersion == 0 {
		card.ProtoVersion = 1
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.cards[card.Name]
	if !ok {
		// First registration under this name.
		c := card
		r.cards[card.Name] = &c
		return c
	}

	// Same name + same addr → dedup, refresh fields + timestamp.
	if existing.Addr == card.Addr {
		c := card
		r.cards[card.Name] = &c
		return c
	}

	// Same name + different addr → conflict. Resolve via tie-breaker.
	// Loser is not registered (but the existing canonical stays unless the
	// challenger wins).
	if !cardWinsConflict(card, *existing) {
		r.logger.Warn("agentmesh: name conflict, keeping existing",
			zap.String("name", card.Name),
			zap.String("existing_addr", existing.Addr),
			zap.String("rejected_addr", card.Addr),
		)
		return *existing
	}
	r.logger.Warn("agentmesh: name conflict, replacing with challenger",
		zap.String("name", card.Name),
		zap.String("old_addr", existing.Addr),
		zap.String("new_addr", card.Addr),
	)
	c := card
	r.cards[card.Name] = &c
	return c
}

// cardWinsConflict reports whether challenger should replace incumbent under a
// name conflict. Tie-breaker order (first decider wins):
//  1. higher ProtoVersion
//  2. lower Load
//  3. lexicographically greater Version
//  4. more recent Status == AgentRunning (treat running as "fresher")
//  5. lexicographically smaller Addr (deterministic, avoids split-brain)
func cardWinsConflict(challenger, incumbent AgentCard) bool {
	if challenger.ProtoVersion != incumbent.ProtoVersion {
		return challenger.ProtoVersion > incumbent.ProtoVersion
	}
	if challenger.Load != incumbent.Load {
		return challenger.Load < incumbent.Load
	}
	if challenger.Version != incumbent.Version {
		return challenger.Version > incumbent.Version
	}
	// Running is "fresher" than any non-running state.
	cr := challenger.Status == AgentRunning
	ir := incumbent.Status == AgentRunning
	if cr != ir {
		return cr // running wins
	}
	return challenger.Addr < incumbent.Addr
}

// Get returns the card registered under name, or false.
func (r *Registry) Get(name string) (AgentCard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cards[name]
	if !ok {
		return AgentCard{}, false
	}
	return *c, true
}

// GetByAddr returns the card whose Addr matches.
func (r *Registry) GetByAddr(addr string) (AgentCard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.cards {
		if c.Addr == addr {
			return *c, true
		}
	}
	return AgentCard{}, false
}

// List returns a snapshot of all registered cards.
func (r *Registry) List() []AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentCard, 0, len(r.cards))
	for _, c := range r.cards {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListRunning returns cards with Status == AgentRunning.
func (r *Registry) ListRunning() []AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentCard, 0, len(r.cards))
	for _, c := range r.cards {
		if c.Status == AgentRunning {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Deregister removes a card by name. It is a no-op if the name is unknown.
func (r *Registry) Deregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cards, name)
}

// UpdateLoad sets the current load on the named agent. No-op if unknown.
func (r *Registry) UpdateLoad(name string, load int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cards[name]; ok {
		c.Load = load
	}
}

// String is a small debug aid.
func (r *Registry) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return fmt.Sprintf("Registry(%d agents)", len(r.cards))
}

// nowVal is overridable in tests; production uses time.Now.
var nowVal = func() time.Time { return time.Now() }
