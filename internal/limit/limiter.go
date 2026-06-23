package limit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/config"
	"dolphin/internal/event"
)

// Limiter checks and records LLM usage limits.
// It implements hook.Handler for synchronous pre-check.
type Limiter struct {
	store       Store
	cfg         *config.Config
	logger      *zap.Logger
	eventBus    *event.Bus
	modelLimits map[string]PerModelLimit // "section/name" → per-model limit config
	alerted     map[string]bool          // "key/soft|hard" → already notified this cycle
}

// ClearAlerted clears the alerted-after-reset tracking, so limits re-alert
// on the next cycle.
func (l *Limiter) ClearAlerted() {
	l.alerted = make(map[string]bool)
}

// ResetUsage resets usage counters matching the given target and clears the
// corresponding alerted entries so those limits can fire alerts again.
//
// target semantics:
//   - "" (empty): resets everything (global + all per-model).
//   - "deepseek": resets all per-model keys for configured models whose
//     qualified name starts with "deepseek" — vendor-scoped.
//   - "deepseek-v4-flash": resets per-model keys whose qualified name ends
//     with "/deepseek-v4-flash" (short-name match across providers).
//   - "deepseek_anthropic/deepseek-v4-flash": resets the exact qualified model.
//
// Global counters (llm.requests etc.) are only reset by the empty target,
// since they are shared across all models and should not be zeroed by a
// single model/vendor reset. Returns the number of keys reset.
func (l *Limiter) ResetUsage(target string) (int, error) {
	// Empty target: reset everything (global + per-model) and clear all alerts.
	if target == "" {
		if err := l.store.Reset(""); err != nil {
			return 0, err
		}
		l.alerted = make(map[string]bool)
		// Count is unknown for a full Reset(""); report 0 meaning "all".
		return 0, nil
	}

	// Resolve the target to concrete qualified-name prefixes. Each becomes a
	// "llm.model.<qualified>." store-key prefix.
	prefixes := l.expandResetPrefix(target)
	storePrefixes := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		storePrefixes = append(storePrefixes, "llm.model."+p)
	}

	// Snapshot current keys so we can count what gets removed and clear the
	// matching alerted entries precisely.
	all, err := l.store.GetAll()
	if err != nil {
		return 0, err
	}

	n := 0
	for _, sp := range storePrefixes {
		if err := l.store.Reset(sp); err != nil {
			return n, err
		}
		for k := range all {
			if strings.HasPrefix(k, sp) {
				n++
				delete(all, k)
			}
		}
	}

	// Clear alerted entries whose underlying store key was reset.
	for ak := range l.alerted {
		for _, sp := range storePrefixes {
			if strings.HasPrefix(ak, sp) {
				delete(l.alerted, ak)
				break
			}
		}
	}
	return n, nil
}

// expandResetPrefix turns a user-supplied reset target into the qualified-name
// prefixes to reset. Empty input returns [""] meaning "everything".
// A target containing "/" is treated as an exact qualified name.
// Otherwise it is matched as a short name suffix against configured models;
// if no configured model matches, the raw target is returned so the caller
// still resets whatever keys happen to share that prefix.
func (l *Limiter) expandResetPrefix(target string) []string {
	if target == "" {
		return []string{""}
	}
	if strings.Contains(target, "/") {
		return []string{target}
	}
	var matches []string
	for qualified := range l.modelLimits {
		if strings.HasSuffix(qualified, "/"+target) {
			matches = append(matches, qualified)
		}
	}
	if len(matches) == 0 {
		// Fall back: treat as a raw vendor prefix (e.g. "deepseek" matches
		// "deepseek_anthropic/..." and "deepseek_openai/...").
		return []string{target}
	}
	return matches
}

// PerModelLimit stores the per-model limit overrides.
type PerModelLimit struct {
	HardRequests   int64
	HardTokens     int64
	SoftRequests   int64
	SoftTokens     int64
	MaxConcurrency int
}

// NewLimiter creates a Limiter.
func NewLimiter(store Store, cfg *config.Config, eventBus *event.Bus, logger *zap.Logger) *Limiter {
	l := &Limiter{
		store:       store,
		cfg:         cfg,
		eventBus:    eventBus,
		logger:      logger,
		modelLimits: make(map[string]PerModelLimit),
		alerted:     make(map[string]bool),
	}
	l.scanModelLimits()
	return l
}

// scanModelLimits reads all provider sections for models[].limit and builds a per-model map.
func (l *Limiter) scanModelLimits() {
	providerSections := discoverProviderSections(l.cfg)
	for _, section := range providerSections {
		prefix := "llm." + section + ".models."
		seen := make(map[int]bool)
		for _, key := range l.cfg.Keys() {
			if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, ".name") {
				continue
			}
			remain := strings.TrimPrefix(key, prefix)
			remain = strings.TrimSuffix(remain, ".name")
			idx, err := strconv.Atoi(remain)
			if err != nil || seen[idx] {
				continue
			}
			seen[idx] = true
			name := l.cfg.GetString(key)
			if name == "" {
				continue
			}
			limitPrefix := prefix + remain + ".limit"
			hardRequests := ReadHardLimit(l.cfg, limitPrefix+".max_requests")
			hardTokens := ReadHardLimit(l.cfg, limitPrefix+".max_total_tokens")
			softRequests := ReadSoftLimit(l.cfg, limitPrefix+".max_requests")
			softTokens := ReadSoftLimit(l.cfg, limitPrefix+".max_total_tokens")
			maxConcurrency := l.cfg.GetInt(limitPrefix + ".max_concurrency")
			if hardRequests > 0 || hardTokens > 0 || softRequests > 0 || softTokens > 0 || maxConcurrency > 0 {
				qualified := section + "/" + name
				l.modelLimits[qualified] = PerModelLimit{
					HardRequests:   hardRequests,
					HardTokens:     hardTokens,
					SoftRequests:   softRequests,
					SoftTokens:     softTokens,
					MaxConcurrency: maxConcurrency,
				}
				l.logger.Info("limit: loaded per-model limit",
					zap.String("model", qualified),
					zap.Int64("hard_requests", hardRequests),
					zap.Int64("hard_tokens", hardTokens),
					zap.Int64("soft_requests", softRequests),
					zap.Int64("soft_tokens", softTokens),
					zap.Int("max_concurrency", maxConcurrency),
				)
			}
		}
	}
}

// discoverProviderSections finds all provider section names (e.g. "deepseek_anthropic").
func discoverProviderSections(cfg *config.Config) []string {
	var sections []string
	seen := make(map[string]bool)
	for _, key := range cfg.Keys() {
		before, ok := strings.CutSuffix(key, ".api_key")
		if !ok {
			continue
		}
		name, ok := strings.CutPrefix(before, "llm.")
		if !ok || name == "" || strings.Contains(name, ".") {
			continue
		}
		if !seen[name] {
			sections = append(sections, name)
			seen[name] = true
		}
	}
	return sections
}

// ---------------------------------------------------------------------------
// hook.Handler
// ---------------------------------------------------------------------------

// Name returns the handler name.
func (l *Limiter) Name() string { return "limit" }

// Handle implements hook.Handler for synchronous pre-check.
func (l *Limiter) Handle(ctx context.Context, e event.Event) error {
	if e.Type != event.EventCheckLLM {
		return nil
	}
	return l.checkLLM(ctx, e)
}

// ---------------------------------------------------------------------------
// limit checking
// ---------------------------------------------------------------------------

type limitDef struct {
	key     string // metric key prefix for store lookup
	hard    int64
	soft    int64
	display string // human-readable name
}

func (l *Limiter) checkLLM(ctx context.Context, e event.Event) error {
	model, _ := e.Payload["model"].(string)
	modelStr := model // local copy

	// Gather all limits.
	limits := l.gatherLimits(modelStr)

	var (
		anySoft  bool
		hardErrs []string
	)
	for _, lm := range limits {
		current, err := l.store.Get(lm.key)
		if err != nil {
			l.logger.Warn("limit: store get failed", zap.String("key", lm.key), zap.Error(err))
			// Fail-closed: when a hard limit is configured but the store is
			// unavailable, treat the limit as exceeded to avoid silent overuse.
			if lm.hard > 0 {
				hardErrs = append(hardErrs, lm.display)
			}
			continue
		}

		if lm.hard > 0 && current >= lm.hard {
			alertKey := lm.key + "/hard"
			if !l.alerted[alertKey] {
				l.alerted[alertKey] = true
				l.eventBus.Publish(ctx, event.Event{
					Type:      event.EventLimitHardBlock,
					Timestamp: time.Now(),
					SessionID: e.SessionID,
					Payload: map[string]any{
						"metric":  lm.display,
						"current": current,
						"hard":    lm.hard,
						"model":   model,
					},
				})
				l.logger.Warn("limit: hard limit exceeded",
					zap.String("metric", lm.display),
					zap.Int64("current", current),
					zap.Int64("hard", lm.hard),
					zap.String("model", model),
				)
			}
			hardErrs = append(hardErrs, fmt.Sprintf("%s (%d/%d)", lm.display, current, lm.hard))
			continue
		}

		if soft := lm.soft; soft > 0 && current >= soft {
			// Soft limit exceeded — warn only, don't block.
			alertKey := lm.key + "/soft"
			if !l.alerted[alertKey] {
				l.alerted[alertKey] = true
				anySoft = true
				l.eventBus.Publish(ctx, event.Event{
					Type:      event.EventLimitSoftWarn,
					Timestamp: time.Now(),
					SessionID: e.SessionID,
					Payload: map[string]any{
						"metric":  lm.display,
						"current": current,
						"soft":    soft,
						"hard":    lm.hard,
						"model":   model,
					},
				})
				l.logger.Warn("limit: soft limit exceeded",
					zap.String("metric", lm.display),
					zap.Int64("current", current),
					zap.Int64("soft", soft),
					zap.String("model", model),
				)
			}
		}
	}

	if len(hardErrs) > 0 {
		return fmt.Errorf("limit reached: %s", strings.Join(hardErrs, "; "))
	}
	if anySoft {
		l.logger.Info("limit: soft limits exceeded, call allowed")
	}
	return nil
}

// gatherLimits collects all applicable limits (global + per-model).
func (l *Limiter) gatherLimits(model string) []limitDef {
	var out []limitDef

	// --- Global limits ---
	out = append(out, l.globalLimit("max_requests", "llm.requests", "requests")...)
	out = append(out, l.globalLimit("max_total_tokens", "llm.total_tokens", "total tokens")...)
	out = append(out, l.globalLimit("max_input_tokens", "llm.input_tokens", "input tokens")...)
	out = append(out, l.globalLimit("max_output_tokens", "llm.output_tokens", "output tokens")...)

	// --- Per-model limits ---
	for qualified, ml := range l.modelLimits {
		// If model from payload is already qualified (contains "/"), match exact.
		// Otherwise match by suffix (short name matches any provider).
		if strings.Contains(model, "/") {
			if qualified != model {
				continue
			}
		} else if !strings.HasSuffix(qualified, "/"+model) {
			continue
		}
		if ml.HardRequests > 0 || ml.SoftRequests > 0 {
			soft := ml.SoftRequests
			if soft <= 0 {
				soft = softDefault(ml.HardRequests)
			}
			out = append(out, limitDef{
				key:     "llm.model." + qualified + ".requests",
				hard:    ml.HardRequests,
				soft:    soft,
				display: "requests (" + qualified + ")",
			})
		}
		if ml.HardTokens > 0 || ml.SoftTokens > 0 {
			soft := ml.SoftTokens
			if soft <= 0 {
				soft = softDefault(ml.HardTokens)
			}
			out = append(out, limitDef{
				key:     "llm.model." + qualified + ".tokens",
				hard:    ml.HardTokens,
				soft:    soft,
				display: "tokens (" + qualified + ")",
			})
		}
	}

	return out
}

func (l *Limiter) globalLimit(configKey, storeKey, display string) []limitDef {
	hard := ReadHardLimit(l.cfg, "llm.limit."+configKey)
	if hard <= 0 {
		return nil
	}
	soft := ReadSoftLimit(l.cfg, "llm.limit."+configKey)
	if soft <= 0 {
		soft = softDefault(hard)
	}
	return []limitDef{{
		key:     storeKey,
		hard:    hard,
		soft:    soft,
		display: display,
	}}
}

// ---------------------------------------------------------------------------
// recording
// ---------------------------------------------------------------------------

// RecordLLM records usage after a successful LLM call.
func (l *Limiter) RecordLLM(model string, inputTokens, outputTokens int) {
	total := int64(inputTokens + outputTokens)
	l.incr("llm.requests", 1)
	if total > 0 {
		l.incr("llm.total_tokens", total)
	}
	if inputTokens > 0 {
		l.incr("llm.input_tokens", int64(inputTokens))
	}
	if outputTokens > 0 {
		l.incr("llm.output_tokens", int64(outputTokens))
	}
	if model != "" {
		l.incr("llm.model."+model+".requests", 1)
		if total > 0 {
			l.incr("llm.model."+model+".tokens", total)
		}
		// Also record to qualified keys so /limit per-model display works
		// when the model name is short (no provider prefix).
		if !strings.Contains(model, "/") {
			for qualified := range l.modelLimits {
				if strings.HasSuffix(qualified, "/"+model) {
					l.incr("llm.model."+qualified+".requests", 1)
					if total > 0 {
						l.incr("llm.model."+qualified+".tokens", total)
					}
				}
			}
		}
	}
}

// incr increments a counter, logging (not failing) on store errors — usage
// counters are best-effort and must not break the LLM call path.
func (l *Limiter) incr(key string, delta int64) {
	if _, err := l.store.Increment(key, delta); err != nil && l.logger != nil {
		l.logger.Warn("limit: increment failed", zap.String("key", key), zap.Error(err))
	}
}

// Store returns the underlying store (for inspection by commands / tests).
func (l *Limiter) Store() Store { return l.store }

// Config returns the underlying config (for inspection by commands).
func (l *Limiter) Config() *config.Config { return l.cfg }

// ModelLimits returns the per-model limit map (for inspection).
func (l *Limiter) ModelLimits() map[string]PerModelLimit {
	out := make(map[string]PerModelLimit, len(l.modelLimits))
	for k, v := range l.modelLimits {
		out[k] = v
	}
	return out
}

// ReadHardLimit reads a limit value which can be either a scalar (int) or
// {hard: N, soft: M} YAML object. In both cases returns the hard value.
func ReadHardLimit(cfg *config.Config, key string) int64 {
	// Try as object: key.hard
	if v := cfg.GetInt(key + ".hard"); v > 0 {
		return int64(v)
	}
	// Try as scalar: key
	return int64(cfg.GetInt(key))
}

// ReadSoftLimit reads the soft limit. If not explicitly configured,
// returns 0 (caller should use softDefault(hard)).
func ReadSoftLimit(cfg *config.Config, key string) int64 {
	// Try as object: key.soft
	if v := cfg.GetInt(key + ".soft"); v > 0 {
		return int64(v)
	}
	// If configured as scalar (no .soft), there is no explicit soft config.
	// Return 0, and the caller uses softDefault(hard).
	return 0
}

// softDefault returns the default soft threshold as 80% of hard.
func softDefault(hard int64) int64 {
	if hard <= 0 {
		return 0
	}
	return hard * 80 / 100
}
