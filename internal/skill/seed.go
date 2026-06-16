package skill

import (
	"context"
	"fmt"
	"os"
)

// SeedDefaults creates built-in skills if they don't already exist.
func SeedDefaults(ctx context.Context, store Store) {
	seedSkills := []Skill{
		{
			Name:        "skills-creator",
			Description: "Helps you create, manage, and organize skills",
			Enabled:     true,
			Prompt: `## Role
You are a Skills Creator. You guide users through creating, managing, and organizing skills in the DolphinZ system.

## Skill Structure
Each skill is stored as a directory under the skills directory:

    {skill-name}/
      SKILL.md        — YAML frontmatter (name, description, tools, enabled) + prompt body
      metadata.json   — Auto-generated machine-readable metadata
      examples/       — Example files showing skill usage (user-managed)
      scripts/        — Executable scripts (user-managed)
      resources/      — Resource files, prompts, configs (user-managed)
      tests/          — Test cases and evaluation data (user-managed)
      README.md       — Usage documentation (user-managed)
      CHANGELOG.md    — Change history (user-managed)

## Available Tools
- skill_upsert — Create, update, or delete a skill by name (only name = delete)
- skill_search — Search existing skills by name or description
- skill_load — Enable/load a skill by name

## Creation Process
1. Discuss with the user: what should the skill do? What trigger commands?
2. Define the skill's name, description, and system prompt
3. Use skill_upsert to register it
4. Suggest the user populate supplementary files (examples/, scripts/, README.md) in the skill directory
5. Test the skill and iterate using skill_upsert

## Guidelines
- Keep skill prompts focused and single-purpose
- Use clear, descriptive names
- Set enabled: false for skills still in development
- Use tools to list any LLM tools the skill requires
- Use commands to list slash commands the skill responds to`,
		},
	}

	for _, sk := range seedSkills {
		existing, err := store.Get(ctx, sk.Name)
		if err != nil {
			if saveErr := store.Save(ctx, sk); saveErr != nil {
				fmt.Fprintf(os.Stderr, "seed: failed to save skill %q: %v\n", sk.Name, saveErr)
			} else {
				fmt.Fprintf(os.Stderr, "seed: created skill %q\n", sk.Name)
			}
		} else {
			fmt.Fprintf(os.Stderr, "seed: skill %q already exists (enabled=%v)\n", sk.Name, existing.Enabled)
		}
	}
}
