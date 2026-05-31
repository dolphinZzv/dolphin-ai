package skill

import (
	"context"
)

// Skill defines a reusable capability with commands and prompt context.
type Skill struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Prompt      string   `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Tools       []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	Enabled     bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Commands    []string `json:"commands,omitempty" yaml:"commands,omitempty"`
}

// Store persists and retrieves skills.
type Store interface {
	List(ctx context.Context) ([]Skill, error)
	Get(ctx context.Context, name string) (*Skill, error)
	Save(ctx context.Context, sk Skill) error
	Delete(ctx context.Context, name string) error
	Search(ctx context.Context, query string) ([]Skill, error)
}
