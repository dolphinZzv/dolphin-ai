package command

import (
	"context"
	"strings"

	"dolphin/internal/skill"

	"github.com/spf13/cobra"
)

// RegisterSkills registers the /skills command group.
func RegisterSkills(r *Registry, skillStore interface {
	List(ctx context.Context) ([]skill.Skill, error)
	Get(ctx context.Context, name string) (*skill.Skill, error)
	Save(ctx context.Context, sk skill.Skill) error
}) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "skills",
	}, "command.skills_manage")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, err := skillStore.List(context.Background())
			if err != nil {
				cmd.Printf("list error: %v\n", err)
				return nil
			}
			if len(skills) == 0 {
				cmd.Println("No skills available")
				return nil
			}
			if RenderModeFrom(cmd) == "markdown" {
				cmd.Print("**Available skills:**\n\n")
				cmd.Println("| Name | Status |")
				cmd.Println("|------|--------|")
				for _, sk := range skills {
					enabled := "disabled"
					if sk.Enabled {
						enabled = "✅ enabled"
					}
					cmd.Printf("| %s | %s |\n", sk.Name, enabled)
				}
			} else {
				cmd.Println("Available skills:")
				for _, sk := range skills {
					enabled := "disabled"
					if sk.Enabled {
						enabled = "enabled"
					}
					cmd.Printf("  %s (%s)\n", sk.Name, enabled)
				}
			}
			cmd.Printf("  (total: %d skills)\n", len(skills))
			return nil
		},
	}, "command.skills_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "enable [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			sk, err := skillStore.Get(context.Background(), name)
			if err != nil {
				cmd.Printf("skill %q not found\n", name)
				return nil
			}
			sk.Enabled = true
			skillStore.Save(context.Background(), *sk)
			cmd.Printf("skill %q enabled\n", name)
			return nil
		},
	}, "command.skills_enable_cmd"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "disable [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			sk, err := skillStore.Get(context.Background(), name)
			if err != nil {
				cmd.Printf("skill %q not found\n", name)
				return nil
			}
			sk.Enabled = false
			skillStore.Save(context.Background(), *sk)
			cmd.Printf("skill %q disabled\n", name)
			return nil
		},
	}, "command.skills_disable_cmd"))

	r.Register(cmd)
}
