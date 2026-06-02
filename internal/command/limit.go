package command

import (
	"sort"

	"dolphin/internal/config"
	"dolphin/internal/limit"

	"github.com/spf13/cobra"
)

// limitStore is the subset of Store needed for the /limit command.
type limitStore interface {
	Get(key string) (int64, error)
	GetAll() (map[string]int64, error)
}

// RegisterLimit registers the /limit command for viewing usage and limits.
func RegisterLimit(r *Registry, limiter *limit.Limiter) {
	if limiter == nil {
		return
	}

	cmd := &cobra.Command{
		Use:   "limit",
		Short: "Show LLM usage and limit status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printLimitStatus(cmd, limiter)
		},
	}

	r.Register(cmd)
}

func printLimitStatus(cmd *cobra.Command, limiter *limit.Limiter) error {
	store := limiter.Store()
	usage, err := store.GetAll()
	if err != nil {
		cmd.Printf("error reading usage: %v\n", err)
		return nil
	}

	cfg := limiter.Config()
	modelLimits := limiter.ModelLimits()

	cmd.Println("=== Global Limits ===")
	printGlobalLimit(cmd, cfg, "max_requests", "requests", "llm.requests", usage)
	printGlobalLimit(cmd, cfg, "max_input_tokens", "input tokens", "llm.input_tokens", usage)
	printGlobalLimit(cmd, cfg, "max_output_tokens", "output tokens", "llm.output_tokens", usage)
	printGlobalLimit(cmd, cfg, "max_total_tokens", "total tokens", "llm.total_tokens", usage)

	if len(modelLimits) > 0 {
		cmd.Println("\n=== Per-Model Limits ===")
		models := make([]string, 0, len(modelLimits))
		for qualified := range modelLimits {
			models = append(models, qualified)
		}
		sort.Strings(models)
		for _, qualified := range models {
			ml := modelLimits[qualified]
			// qualified format: "section/name" — use directly as store key suffix
			// to match what RecordLLM writes.
			reqKey := "llm.model." + qualified + ".requests"
			tokKey := "llm.model." + qualified + ".tokens"
			reqCurrent := usage[reqKey]
			tokCurrent := usage[tokKey]

			cmd.Printf("  %s:\n", qualified)
			if ml.HardRequests > 0 || ml.SoftRequests > 0 {
				softReq := ml.SoftRequests
				if softReq <= 0 {
					softReq = ml.HardRequests * 80 / 100
				}
				if ml.HardRequests > 0 {
					cmd.Printf("    requests:  hard=%-8d soft=%-8d current=%d\n", ml.HardRequests, softReq, reqCurrent)
				} else {
					cmd.Printf("    requests:  hard=%-8s soft=%-8d current=%d\n", "-", softReq, reqCurrent)
				}
			}
			if ml.HardTokens > 0 || ml.SoftTokens > 0 {
				softTok := ml.SoftTokens
				if softTok <= 0 {
					softTok = ml.HardTokens * 80 / 100
				}
				if ml.HardTokens > 0 {
					cmd.Printf("    tokens:    hard=%-8d soft=%-8d current=%d\n", ml.HardTokens, softTok, tokCurrent)
				} else {
					cmd.Printf("    tokens:    hard=%-8s soft=%-8d current=%d\n", "-", softTok, tokCurrent)
				}
			}
			if ml.HardRequests == 0 && ml.HardTokens == 0 && ml.SoftRequests == 0 && ml.SoftTokens == 0 {
				cmd.Printf("    (no limits configured)\n")
			}
		}
	}

	cmd.Println()
	totalRequests := usage["llm.requests"]
	cmd.Printf("Total requests: %d\n", totalRequests)
	return nil
}

func printGlobalLimit(cmd *cobra.Command, cfg *config.Config, configKey, display, storeKey string, usage map[string]int64) {
	hard := limit.ReadHardLimit(cfg, "llm.limit."+configKey)
	current := usage[storeKey]
	if hard <= 0 {
		cmd.Printf("  %s: current=%d (no limit)\n", display, current)
		return
	}
	soft := limit.ReadSoftLimit(cfg, "llm.limit."+configKey)
	if soft <= 0 {
		soft = hard * 80 / 100
	}
	cmd.Printf("  %s: hard=%-8d soft=%-8d current=%d\n", display, hard, soft, current)
}
