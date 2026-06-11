package command

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"dolphin/internal/i18n"
	"github.com/spf13/cobra"
)

func TestLangListOutput(t *testing.T) {
	i18n.SetLang("zh")

	// Replicate runLangList logic step by step
	current := i18n.Lang()
	names := make([]string, 0, len(supportedLangs))
	for code := range supportedLangs {
		names = append(names, code)
	}
	sort.Strings(names)

	t.Logf("current lang: %q", current)
	t.Logf("names after sort: %v", names)
	t.Logf("supportedLangs: %v", supportedLangs)

	var buf bytes.Buffer
	for _, code := range names {
		mark := "  "
		suffix := ""
		if code == current {
			mark = ">>"
			suffix = " " + i18n.T("command.lang_active")
		}
		line := fmt.Sprintf("%s %-6s %s%s\n", mark, code, supportedLangs[code], suffix)
		buf.WriteString(line)
		t.Logf("  code=%q mark=%q suffix=%q line=%q", code, mark, suffix, line)
	}

	output := strings.TrimRight(buf.String(), "\n")
	t.Logf("final output:\n%s", output)
	t.Logf("final output bytes: %q", output)

	if !strings.Contains(output, "en") {
		t.Error("output missing 'en'")
	}
	if !strings.Contains(output, "zh") {
		t.Error("output missing 'zh'")
	}
}

func TestLangListViaCobra(t *testing.T) {
	i18n.SetLang("zh")

	r := NewRegistry(nil, nil)
	RegisterLang(r)

	var buf bytes.Buffer
	r.root.SetOut(&buf)
	r.root.SetArgs([]string{"lang"})
	r.root.SetContext(nil)
	_, err := r.root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC error: %v", err)
	}

	output := strings.TrimRight(buf.String(), "\n")
	t.Logf("Cobra output:\n%s", output)
	t.Logf("Cobra output bytes: %q", output)

	if !strings.Contains(output, "zh") {
		t.Error("cobra output missing 'zh'")
	}
}

func TestLangListViaExecute(t *testing.T) {
	i18n.SetLang("zh")

	r := NewRegistry(nil, nil)
	RegisterLang(r)

	output := r.Execute(context.Background(), "lang", "")
	t.Logf("Execute output:\n%s", output)
	t.Logf("Execute output bytes: %q", output)

	if !strings.Contains(output, "zh") {
		t.Error("Execute output missing 'zh'")
	}
}

func TestRunLangUseDirect(t *testing.T) {
	original := i18n.Lang()
	defer i18n.SetLang(original)

	i18n.SetLang("en")

	t.Run("valid switch", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := WithI18nShort(&cobra.Command{Use: "test"}, "")
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"zh"})

		err := runLangUse(cmd, []string{"zh"})
		if err != nil {
			t.Fatal(err)
		}
		if i18n.Lang() != "zh" {
			t.Errorf("expected zh, got %s", i18n.Lang())
		}
	})

	t.Run("invalid code", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := WithI18nShort(&cobra.Command{Use: "test"}, "")
		cmd.SetOut(&buf)

		err := runLangUse(cmd, []string{"fr"})
		if err != nil {
			t.Fatal(err)
		}
		if i18n.Lang() != "zh" {
			t.Errorf("expected zh (unchanged), got %s", i18n.Lang())
		}
	})

	t.Run("trims whitespace from code", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := WithI18nShort(&cobra.Command{Use: "test"}, "")
		cmd.SetOut(&buf)
		i18n.SetLang("en")

		err := runLangUse(cmd, []string{"  zh  "})
		if err != nil {
			t.Fatal(err)
		}
		if i18n.Lang() != "zh" {
			t.Errorf("expected zh after trim, got %s", i18n.Lang())
		}
	})
}
