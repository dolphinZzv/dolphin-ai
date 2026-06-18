package command

import (
	"sort"

	"github.com/spf13/cobra"

	"dolphin/internal/config"
	"dolphin/internal/limit"
)

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

	isMarkdown := RenderModeFrom(cmd) == "markdown"

	if isMarkdown {
		cmd.Print("### Global Limits\n\n")
		cmd.Println("| Limit | Hard | Soft | Current |")
		cmd.Println("|-------|------|------|---------|")
	} else {
		cmd.Println("=== Global Limits ===")
	}
	printGlobalLimit(cmd, cfg, "max_requests", "requests", "llm.requests", usage, isMarkdown)
	printGlobalLimit(cmd, cfg, "max_input_tokens", "input tokens", "llm.input_tokens", usage, isMarkdown)
	printGlobalLimit(cmd, cfg, "max_output_tokens", "output tokens", "llm.output_tokens", usage, isMarkdown)
	printGlobalLimit(cmd, cfg, "max_total_tokens", "total tokens", "llm.total_tokens", usage, isMarkdown)

	if len(modelLimits) > 0 {
		if isMarkdown {
			cmd.Print("\n### Per-Model Limits\n\n")
			cmd.Println("| Model | Limit | Hard | Soft | Current |")
			cmd.Println("|-------|-------|------|------|---------|")
		} else {
			cmd.Println("\n=== Per-Model Limits ===")
		}
		models := make([]string, 0, len(modelLimits))
		for qualified := range modelLimits {
			models = append(models, qualified)
		}
		sort.Strings(models)
		for _, qualified := range models {
			ml := modelLimits[qualified]
			reqKey := "llm.model." + qualified + ".requests"
			tokKey := "llm.model." + qualified + ".tokens"
			reqCurrent := usage[reqKey]
			tokCurrent := usage[tokKey]

			if isMarkdown {
				cmd.Printf("| **%s** | | | | |\n", qualified)
				if ml.HardRequests > 0 || ml.SoftRequests > 0 {
					softReq := ml.SoftRequests
					if softReq <= 0 {
						softReq = ml.HardRequests * 80 / 100
					}
					if ml.HardRequests > 0 {
						cmd.Printf("| | requests | %d | %d | %d |\n", ml.HardRequests, softReq, reqCurrent)
					} else {
						cmd.Printf("| | requests | - | %d | %d |\n", softReq, reqCurrent)
					}
				}
				if ml.HardTokens > 0 || ml.SoftTokens > 0 {
					softTok := ml.SoftTokens
					if softTok <= 0 {
						softTok = ml.HardTokens * 80 / 100
					}
					if ml.HardTokens > 0 {
						cmd.Printf("| | tokens | %d | %d | %d |\n", ml.HardTokens, softTok, tokCurrent)
					} else {
						cmd.Printf("| | tokens | - | %d | %d |\n", softTok, tokCurrent)
					}
				}
				if ml.HardRequests == 0 && ml.HardTokens == 0 && ml.SoftRequests == 0 && ml.SoftTokens == 0 {
					cmd.Println("| | (no limits configured) | | | |")
				}
			} else {
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
	}

	if isMarkdown {
		totalRequests := usage["llm.requests"]
		cmd.Printf("\n**Total requests:** %d\n", totalRequests)
	} else {
		cmd.Println()
		totalRequests := usage["llm.requests"]
		cmd.Printf("Total requests: %d\n", totalRequests)
	}
	return nil
}

func printGlobalLimit(cmd *cobra.Command, cfg *config.Config, configKey, display, storeKey string, usage map[string]int64, isMarkdown bool) {
	hard := limit.ReadHardLimit(cfg, "llm.limit."+configKey)
	current := usage[storeKey]
	if hard <= 0 {
		if isMarkdown {
			cmd.Printf("| %s | - | - | %d |\n", display, current)
		} else {
			cmd.Printf("  %s: current=%d (no limit)\n", display, current)
		}
		return
	}
	soft := limit.ReadSoftLimit(cfg, "llm.limit."+configKey)
	if soft <= 0 {
		soft = hard * 80 / 100
	}
	if isMarkdown {
		cmd.Printf("| %s | %d | %d | %d |\n", display, hard, soft, current)
	} else {
		cmd.Printf("  %s: hard=%-8d soft=%-8d current=%d\n", display, hard, soft, current)
	}
}
