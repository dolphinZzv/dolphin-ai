package cmd

import (
	"dolphin/internal/skill"

	"github.com/spf13/cobra"
)

func NewSkillsCmd() *cobra.Command {
	return skill.SkillsCommandWithConfigPath(cfgFile)
}
