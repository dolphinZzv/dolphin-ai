package command

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"dolphin/internal/config"
	"dolphin/internal/limit"
)

// formatCount renders an integer with k/m/b/t suffixes for compact display.
func formatCount(n int64) string {
	switch {
	case n < 0:
		return fmt.Sprintf("%d", n)
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		v := float64(n) / 1000
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0fk", v)
		}
		return fmt.Sprintf("%.1fk", v)
	case n < 1_000_000_000:
		v := float64(n) / 1_000_000
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0fm", v)
		}
		return fmt.Sprintf("%.1fm", v)
	case n < 1_000_000_000_000:
		v := float64(n) / 1_000_000_000
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0fb", v)
		}
		return fmt.Sprintf("%.1fb", v)
	default:
		v := float64(n) / 1_000_000_000_000
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0ft", v)
		}
		return fmt.Sprintf("%.1ft", v)
	}
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
						cmd.Printf("| | requests | %s | %s | %s |\n", formatCount(ml.HardRequests), formatCount(softReq), formatCount(reqCurrent))
					} else {
						cmd.Printf("| | requests | - | %s | %s |\n", formatCount(softReq), formatCount(reqCurrent))
					}
				}
				if ml.HardTokens > 0 || ml.SoftTokens > 0 {
					softTok := ml.SoftTokens
					if softTok <= 0 {
						softTok = ml.HardTokens * 80 / 100
					}
					if ml.HardTokens > 0 {
						cmd.Printf("| | tokens | %s | %s | %s |\n", formatCount(ml.HardTokens), formatCount(softTok), formatCount(tokCurrent))
					} else {
						cmd.Printf("| | tokens | - | %s | %s |\n", formatCount(softTok), formatCount(tokCurrent))
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
						cmd.Printf("    requests:  hard=%-8s soft=%-8s current=%s\n", formatCount(ml.HardRequests), formatCount(softReq), formatCount(reqCurrent))
					} else {
						cmd.Printf("    requests:  hard=%-8s soft=%-8s current=%s\n", "-", formatCount(softReq), formatCount(reqCurrent))
					}
				}
				if ml.HardTokens > 0 || ml.SoftTokens > 0 {
					softTok := ml.SoftTokens
					if softTok <= 0 {
						softTok = ml.HardTokens * 80 / 100
					}
					if ml.HardTokens > 0 {
						cmd.Printf("    tokens:    hard=%-8s soft=%-8s current=%s\n", formatCount(ml.HardTokens), formatCount(softTok), formatCount(tokCurrent))
					} else {
						cmd.Printf("    tokens:    hard=%-8s soft=%-8s current=%s\n", "-", formatCount(softTok), formatCount(tokCurrent))
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
		cmd.Printf("\n**Total requests:** %s\n", formatCount(totalRequests))
	} else {
		cmd.Println()
		totalRequests := usage["llm.requests"]
		cmd.Printf("Total requests: %s\n", formatCount(totalRequests))
	}
	return nil
}

func printGlobalLimit(cmd *cobra.Command, cfg *config.Config, configKey, display, storeKey string, usage map[string]int64, isMarkdown bool) {
	hard := limit.ReadHardLimit(cfg, "llm.limit."+configKey)
	current := usage[storeKey]
	if hard <= 0 {
		if isMarkdown {
			cmd.Printf("| %s | - | - | %s |\n", display, formatCount(current))
		} else {
			cmd.Printf("  %s: current=%s (no limit)\n", display, formatCount(current))
		}
		return
	}
	soft := limit.ReadSoftLimit(cfg, "llm.limit."+configKey)
	if soft <= 0 {
		soft = hard * 80 / 100
	}
	if isMarkdown {
		cmd.Printf("| %s | %s | %s | %s |\n", display, formatCount(hard), formatCount(soft), formatCount(current))
	} else {
		cmd.Printf("  %s: hard=%-8s soft=%-8s current=%s\n", display, formatCount(hard), formatCount(soft), formatCount(current))
	}
}
