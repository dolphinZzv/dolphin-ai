package brain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Script defines a stored script with content users can execute via slash commands.
type Script struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Content     string // body after frontmatter
}

const scriptDir = "scripts"

func scriptPath(name string) string {
	return filepath.Join(scriptDir, name+".md")
}

func parseScript(data string) (*Script, error) {
	rest, ok := strings.CutPrefix(data, frontmatterDelim)
	if !ok {
		return nil, fmt.Errorf("missing frontmatter delimiter")
	}
	yamlPart, body, ok := strings.Cut(rest, frontmatterDelim)
	if !ok {
		return nil, fmt.Errorf("missing closing frontmatter delimiter")
	}
	body = strings.TrimLeft(body, "\n")

	var s Script
	if err := yaml.Unmarshal([]byte(yamlPart), &s); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("script name is required")
	}
	s.Content = body
	return &s, nil
}

func serializeScript(s Script) (string, error) {
	type front struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
		Enabled     bool   `yaml:"enabled"`
	}
	f := front{Name: s.Name, Description: s.Description, Enabled: s.Enabled}
	yamlData, err := yaml.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("serialize frontmatter: %w", err)
	}
	var sb strings.Builder
	sb.WriteString(frontmatterDelim)
	sb.Write(yamlData)
	sb.WriteString(frontmatterDelim)
	if s.Content != "" {
		sb.WriteString(s.Content)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// ReadScript reads and parses a script from the brain.
func ReadScript(ctx context.Context, b *Brain, name string) (*Script, error) {
	if name == "" {
		return nil, fmt.Errorf("script name is required")
	}
	data, err := b.Read(ctx, scriptPath(name))
	if err != nil {
		return nil, err
	}
	return parseScript(data)
}

// WriteScript serializes and writes a script to the brain.
func WriteScript(ctx context.Context, b *Brain, s Script) error {
	if s.Name == "" {
		return fmt.Errorf("script name is required")
	}
	data, err := serializeScript(s)
	if err != nil {
		return err
	}
	return b.Write(ctx, scriptPath(s.Name), "script: "+s.Name, data)
}

// ListScripts lists all scripts stored in the brain.
func ListScripts(ctx context.Context, b *Brain) ([]Script, error) {
	files, err := b.List(ctx)
	if err != nil {
		return nil, err
	}

	var scripts []Script
	prefix := scriptDir + "/"
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) || !strings.HasSuffix(f, ".md") {
			continue
		}
		if f == prefix+"index.md" {
			continue
		}
		s, err := ReadScript(ctx, b, strings.TrimSuffix(strings.TrimPrefix(f, prefix), ".md"))
		if err != nil {
			continue
		}
		scripts = append(scripts, *s)
	}
	return scripts, nil
}

// DeleteScript deletes a script from the brain.
func DeleteScript(ctx context.Context, b *Brain, name string) error {
	if name == "" {
		return fmt.Errorf("script name is required")
	}
	return b.Delete(ctx, scriptPath(name))
}
