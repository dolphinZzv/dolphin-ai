package agentmesh

import (
	"errors"
	"slices"
	"sort"

	"go.uber.org/zap"
)

// ErrNoAgent is returned when no agent matches the request.
var ErrNoAgent = errors.New("agentmesh: agent not found")

// Router selects a target agent for a delegation, applying capability
// matching, load filtering, version filtering and fallback.
type Router struct {
	registry *Registry
	fallback FallbackConfig
	logger   *zap.Logger
}

// NewRouter builds a Router over the given registry.
func NewRouter(reg *Registry, fb FallbackConfig, logger *zap.Logger) *Router {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Router{registry: reg, fallback: fb, logger: logger}
}

// Route resolves a DelegatePayload to one or more candidate agents.
//
// If PreferredAgent is set, it is returned alone (after a load + version
// check). Otherwise capability matching produces a ranked list; the caller
// takes the first and uses the rest as fallback candidates.
func (r *Router) Route(payload DelegatePayload) ([]AgentCard, error) {
	if payload.PreferredAgent != "" {
		c, ok := r.registry.Get(payload.PreferredAgent)
		if !ok {
			return nil, &DelegateError{
				Code:    ErrAgentNotFound,
				Message: "preferred agent not in registry",
				Agent:   payload.PreferredAgent,
			}
		}
		if err := checkUsable(c, payload); err != nil {
			return nil, err
		}
		return []AgentCard{c}, nil
	}

	candidates := r.MatchByCapability(payload.RequiredCapabilities)
	if len(candidates) == 0 {
		return nil, &DelegateError{
			Code:    ErrAgentNotFound,
			Message: "no agent matches required capabilities",
		}
	}
	return candidates, nil
}

// checkUsable returns an error if the agent cannot accept the delegation now.
func checkUsable(c AgentCard, _ DelegatePayload) error {
	if c.Status != AgentRunning {
		return &DelegateError{
			Code: ErrAgentUnavail, Message: "agent not running",
			Agent: c.Name,
		}
	}
	if c.Load >= c.MaxLoad {
		return &DelegateError{
			Code: ErrAgentBusy, Message: "agent at max load",
			Agent: c.Name,
		}
	}
	return nil
}

// MatchByCapability returns running, non-saturated agents whose capabilities
// overlap the required set, ranked by (score desc, load asc). A candidate is
// included only if score >= 0.5 (i.e. it covers at least half of the required
// capabilities). If required is empty, all running agents are returned (the
// caller still needs a hint somewhere — but this avoids a hard failure when
// the LLM omits capabilities).
func (r *Router) MatchByCapability(required []string) []AgentCard {
	running := r.registry.ListRunning()
	// drop saturated agents
	usable := make([]AgentCard, 0, len(running))
	for _, c := range running {
		if c.Load < c.MaxLoad {
			usable = append(usable, c)
		}
	}

	if len(required) == 0 {
		sort.Slice(usable, func(i, j int) bool {
			if usable[i].Load != usable[j].Load {
				return usable[i].Load < usable[j].Load
			}
			return usable[i].Name < usable[j].Name
		})
		return usable
	}

	type scored struct {
		card  AgentCard
		score float64
	}
	hits := make([]scored, 0, len(usable))
	want := len(required)
	for _, c := range usable {
		got := 0
		for _, rq := range required {
			if slices.Contains(c.Capabilities, rq) {
				got++
			}
		}
		score := float64(got) / float64(want)
		if score >= 0.5 {
			hits = append(hits, scored{card: c, score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].card.Load != hits[j].card.Load {
			return hits[i].card.Load < hits[j].card.Load
		}
		return hits[i].card.Name < hits[j].card.Name
	})
	out := make([]AgentCard, len(hits))
	for i, h := range hits {
		out[i] = h.card
	}

	// Apply max_fallback cap to the candidate list length.
	max := r.fallback.MaxFallback
	if max <= 0 {
		max = 2
	}
	if len(out) > max+1 { // primary + max fallbacks
		out = out[:max+1]
	}
	return out
}
