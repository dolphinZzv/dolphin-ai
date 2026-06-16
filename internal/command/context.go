package command

import (
	"context"
	"strings"

	appctx "dolphin/internal/context"

	"github.com/spf13/cobra"
)

// RegisterContext registers the /context command.
// regFn returns the shared section registry (lazy, nil until boot is complete).
func RegisterContext(r *Registry, regFn func() *appctx.Registry) {
	r.Register(WithI18nShort(&cobra.Command{
		Use: "context [all|name]",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := regFn()
			if reg == nil {
				cmd.Println("(context not yet initialized)")
				return nil
			}

			if len(args) == 0 {
				return listSections(cmd, reg)
			}

			arg := args[0]
			if arg == "all" {
				return showAll(cmd, reg)
			}

			return showSection(cmd, reg, arg)
		},
	}, "command.context_desc"))
}

func listSections(cmd *cobra.Command, reg *appctx.Registry) error {
	sections := reg.Sections()
	if len(sections) == 0 {
		cmd.Println("(no sections)")
		return nil
	}
	for _, s := range sections {
		cmd.Printf("%d) %s\n", s.Index()+1, s.Name())
	}
	cmd.Println("\nUse /context all for full content, or /context <name> for a specific section.")
	return nil
}

func showAll(cmd *cobra.Command, reg *appctx.Registry) error {
	prompt, err := reg.Build(context.Background())
	if err != nil {
		cmd.Printf("Error: %v\n", err)
		return nil
	}
	cmd.Println(prompt)
	return nil
}

func showSection(cmd *cobra.Command, reg *appctx.Registry, name string) error {
	s, ok := reg.ByName(name)
	if !ok {
		var names []string
		for _, sec := range reg.Sections() {
			names = append(names, sec.Name())
		}
		cmd.Printf("Unknown section %q — available: %s\n", name, strings.Join(names, ", "))
		return nil
	}

	content, err := s.BuildContent(context.Background())
	if err != nil {
		cmd.Printf("Error building section %q: %v\n", name, err)
		return nil
	}
	if content == "" {
		cmd.Printf("(section %q is empty)\n", name)
		return nil
	}
	cmd.Println(content)
	return nil
}
