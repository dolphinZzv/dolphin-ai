package command

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"dolphin/internal/agentmesh"
	"dolphin/internal/i18n"
)

// agentLister is the subset of AgentMesh the /agents command needs.
type agentLister interface {
	ListAgents() []agentmesh.AgentCard
	Card() agentmesh.AgentCard
}

// RegisterAgents registers the /agents command.
func RegisterAgents(r *Registry, mgr agentLister) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cards := mgr.ListAgents()
			if len(cards) == 0 {
				cmd.Println(i18n.T("command.agents_none"))
				return nil
			}

			sort.Slice(cards, func(i, j int) bool {
				return cards[i].Name < cards[j].Name
			})

			selfAddr := mgr.Card().Addr
			isMarkdown := RenderModeFrom(cmd) == "markdown"

			if isMarkdown {
				cmd.Println("**" + i18n.T("command.agents_title") + "**\n")
			} else {
				cmd.Println(i18n.T("command.agents_title"))
			}

			for _, a := range cards {
				statusStr := string(a.Status)
				loadStr := fmt.Sprintf("%d/%d", a.Load, a.MaxLoad)

				if isMarkdown {
					nameLine := a.Name
					if a.Addr == selfAddr {
						nameLine += " *(" + i18n.T("command.agents_self") + ")*"
					}
					cmd.Printf("### %s\n\n", nameLine)
					cmd.Printf("- **%s**: `%s`\n", i18n.T("command.agents_addr"), a.Addr)
					cmd.Printf("- **%s**: `%s`\n", i18n.T("command.agents_status"), statusStr)
					cmd.Printf("- **%s**: %s\n", i18n.T("command.agents_load"), loadStr)
					if a.Model != "" {
						cmd.Printf("- **%s**: `%s`\n", i18n.T("command.agents_model"), a.Model)
					}
					if len(a.Capabilities) > 0 {
						cmd.Printf("- **%s**: `%s`\n", i18n.T("command.agents_capabilities"), strings.Join(a.Capabilities, "`, `"))
					}
					cmd.Println()
				} else {
					nameLine := a.Name
					if a.Addr == selfAddr {
						nameLine += " (" + i18n.T("command.agents_self") + ")"
					}
					cmd.Printf("\n  %s (%s)\n", nameLine, a.Addr)
					cmd.Printf("    %s: %s\n", i18n.T("command.agents_status"), statusStr)
					cmd.Printf("    %s: %s\n", i18n.T("command.agents_load"), loadStr)
					if a.Model != "" {
						cmd.Printf("    %s: %s\n", i18n.T("command.agents_model"), a.Model)
					}
					if len(a.Capabilities) > 0 {
						cmd.Printf("    %s: %s\n", i18n.T("command.agents_capabilities"), strings.Join(a.Capabilities, ", "))
					}
				}
			}
			return nil
		},
	}, "command.agents_desc")

	r.Register(cmd)
}
