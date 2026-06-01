package brain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Command defines a stored command with instructions the LLM can execute.
type Command struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Content     string // body after frontmatter
}

const commandDir = "commands"
const frontmatterDelim = "---\n"

func commandPath(name string) string {
	return filepath.Join(commandDir, name+".md")
}

func parseCommand(data string) (*Command, error) {
	// Expect: ---\n{frontmatter}---\n{body}
	rest, ok := strings.CutPrefix(data, frontmatterDelim)
	if !ok {
		return nil, fmt.Errorf("missing frontmatter delimiter")
	}
	idx := strings.Index(rest, frontmatterDelim)
	if idx < 0 {
		return nil, fmt.Errorf("missing closing frontmatter delimiter")
	}
	yamlPart := rest[:idx]
	body := strings.TrimLeft(rest[idx+len(frontmatterDelim):], "\n")

	var cmd Command
	if err := yaml.Unmarshal([]byte(yamlPart), &cmd); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if cmd.Name == "" {
		return nil, fmt.Errorf("command name is required")
	}
	cmd.Content = body
	return &cmd, nil
}

func serializeCommand(cmd Command) (string, error) {
	// Only store name, description, enabled in frontmatter.
	type front struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
		Enabled     bool   `yaml:"enabled"`
	}
	f := front{Name: cmd.Name, Description: cmd.Description, Enabled: cmd.Enabled}
	yamlData, err := yaml.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("serialize frontmatter: %w", err)
	}
	var sb strings.Builder
	sb.WriteString(frontmatterDelim)
	sb.Write(yamlData)
	sb.WriteString(frontmatterDelim)
	if cmd.Content != "" {
		sb.WriteString(cmd.Content)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// ReadCommand reads and parses a command from the brain.
func ReadCommand(ctx context.Context, b *Brain, name string) (*Command, error) {
	if name == "" {
		return nil, fmt.Errorf("command name is required")
	}
	data, err := b.Read(ctx, commandPath(name))
	if err != nil {
		return nil, err
	}
	return parseCommand(data)
}

// WriteCommand serializes and writes a command to the brain.
func WriteCommand(ctx context.Context, b *Brain, cmd Command) error {
	if cmd.Name == "" {
		return fmt.Errorf("command name is required")
	}
	data, err := serializeCommand(cmd)
	if err != nil {
		return err
	}
	return b.Write(ctx, commandPath(cmd.Name), "command: "+cmd.Name, data)
}

// ListCommands lists all commands stored in the brain.
func ListCommands(ctx context.Context, b *Brain) ([]Command, error) {
	files, err := b.List(ctx)
	if err != nil {
		return nil, err
	}

	var cmds []Command
	prefix := commandDir + "/"
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) || !strings.HasSuffix(f, ".md") {
			continue
		}
		if f == prefix+"index.md" {
			continue
		}
		cmd, err := ReadCommand(ctx, b, strings.TrimSuffix(strings.TrimPrefix(f, prefix), ".md"))
		if err != nil {
			continue // skip unparseable
		}
		cmds = append(cmds, *cmd)
	}
	return cmds, nil
}

// DeleteCommand deletes a command from the brain.
func DeleteCommand(ctx context.Context, b *Brain, name string) error {
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	return b.Delete(ctx, commandPath(name))
}
