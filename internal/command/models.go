package command

import (
	"context"
	"strings"

	"dolphin/internal/llm"

	"github.com/spf13/cobra"
)

// modelsManager is the subset of *llm.Manager that the models command needs.
type modelsManager interface {
	Models(ctx context.Context) ([]llm.ModelConfig, error)
	ActiveModel() string
	SetActiveModel(name string) error
}

// RegisterModels registers the /models command for listing and switching models.
//
// The underlying provider should be a *llm.Manager for full functionality;
// the command degrades gracefully to read-only if not.
func RegisterModels(r *Registry, provider llm.Provider) {
	mgr, _ := provider.(modelsManager)

	cmd := WithI18nShort(&cobra.Command{
		Use: "models",
	}, "command.models_desc")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			models, err := provider.Models(context.Background())
			if err != nil {
				cmd.Printf("error: %v\n", err)
				return nil
			}
			if len(models) == 0 {
				cmd.Println("No models available")
				return nil
			}

			active := ""
			if mgr != nil {
				active = mgr.ActiveModel()
			}

			if RenderModeFrom(cmd) == "markdown" {
				cmd.Print("**Available models:**\n\n")
				cmd.Println("| Name | Vendor | API Type | Model |")
				cmd.Println("|------|--------|----------|-------|")
				for _, mc := range models {
					suffix := ""
					if mc.Name == active {
						suffix = " 🟢"
					}
					cmd.Printf("| %s | %s | %s | %s%s |\n", mc.Name, mc.Vendor, mc.APIType, mc.Model, suffix)
				}
			} else {
				cmd.Println("Available models:")
				cmd.Printf("   %-30s %-12s %-10s %s\n", "Name", "Vendor", "API Type", "Model")
				for _, mc := range models {
					mark := "  "
					suffix := ""
					if mc.Name == active {
						mark = ">>"
						suffix = " (active)"
					}
					cmd.Printf("%s %-30s %-12s %-10s %s%s\n", mark, mc.Name, mc.Vendor, mc.APIType, mc.Model, suffix)
				}
			}
			cmd.Printf("  (total: %d models)\n", len(models))
			return nil
		},
	}, "command.models_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "use [model]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mgr == nil {
				cmd.Printf("switching models is not supported with the current provider\n")
				return nil
			}
			name := strings.TrimSpace(args[0])
			if err := mgr.SetActiveModel(name); err != nil {
				cmd.Printf("error: %v\n", err)
				return nil
			}
			cmd.Printf("switched to %s\n", name)
			return nil
		},
	}, "command.models_switch"))

	// When called as /models without subcommand, show list.
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		models, err := provider.Models(context.Background())
		if err != nil {
			cmd.Printf("error: %v\n", err)
			return nil
		}
		if len(models) == 0 {
			cmd.Println("No models available")
			return nil
		}

		active := ""
		if mgr != nil {
			active = mgr.ActiveModel()
		}

		if RenderModeFrom(cmd) == "markdown" {
			cmd.Print("**Available models:**\n\n")
			cmd.Println("| Name | Vendor | API Type | Model |")
			cmd.Println("|------|--------|----------|-------|")
			for _, mc := range models {
				suffix := ""
				if mc.Name == active {
					suffix = " 🟢"
				}
				cmd.Printf("| %s | %s | %s | %s%s |\n", mc.Name, mc.Vendor, mc.APIType, mc.Model, suffix)
			}
		} else {
			cmd.Println("Available models:")
			cmd.Printf("   %-30s %-12s %-10s %s\n", "Name", "Vendor", "API Type", "Model")
			for _, mc := range models {
				mark := "  "
				suffix := ""
				if mc.Name == active {
					mark = ">>"
					suffix = " (active)"
				}
				cmd.Printf("%s %-30s %-12s %-10s %s%s\n", mark, mc.Name, mc.Vendor, mc.APIType, mc.Model, suffix)
			}
		}
		cmd.Printf("  (total: %d models)\n", len(models))
		return nil
	}

	r.Register(cmd)
}
