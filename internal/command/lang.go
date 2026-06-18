package command

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"dolphin/internal/i18n"
)

// supportedLangs lists all languages available for switching.
var supportedLangs = map[string]string{
	"en": "English",
	"zh": "中文",
}

// RegisterLang registers the /lang command for listing and switching languages.
func RegisterLang(r *Registry) {
	cmd := WithI18nShort(&cobra.Command{Use: "lang"}, "command.lang_desc")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: runLangList,
	}, "command.lang_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "use [code]",
		Args: cobra.ExactArgs(1),
		RunE: runLangUse,
	}, "command.lang_use"))

	cmd.RunE = runLangList
	r.Register(cmd)
}

func runLangList(cmd *cobra.Command, args []string) error {
	current := i18n.Lang()
	names := make([]string, 0, len(supportedLangs))
	for code := range supportedLangs {
		names = append(names, code)
	}
	sort.Strings(names)

	if RenderModeFrom(cmd) == "markdown" {
		cmd.Println(i18n.T("command.lang_available"))
		cmd.Println()
		cmd.Println("| Code | Name |")
		cmd.Println("|------|------|")
		for _, code := range names {
			suffix := ""
			if code == current {
				suffix = " 🟢"
			}
			cmd.Printf("| %s | %s%s |\n", code, supportedLangs[code], suffix)
		}
	} else {
		cmd.Println(i18n.T("command.lang_available"))
		for _, code := range names {
			mark := "  "
			suffix := ""
			if code == current {
				mark = ">>"
				suffix = " " + i18n.T("command.lang_active")
			}
			cmd.Printf("%s %-6s %s%s\n", mark, code, supportedLangs[code], suffix)
		}
	}
	return nil
}

func runLangUse(cmd *cobra.Command, args []string) error {
	code := strings.TrimSpace(args[0])
	if _, ok := supportedLangs[code]; !ok {
		cmd.Print(i18n.T("command.lang_invalid", code))
		return nil
	}
	i18n.SetLang(code)
	cmd.Print(i18n.T("command.lang_switched", code))
	return nil
}
