package brain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SubscriptionFilter provides optional filtering on event payload.
type SubscriptionFilter struct {
	Path string `yaml:"path,omitempty" json:"path,omitempty"` // glob for file path, used with file.* events
}

// Subscription defines an event-triggered notification stored in the brain.
// When the event matches, the body Content is delivered to the LLM.
type Subscription struct {
	Name         string             `json:"name" yaml:"name"`
	Description  string             `json:"description,omitempty" yaml:"description,omitempty"`
	EventPattern string             `json:"event_pattern" yaml:"event_pattern"` // glob, e.g. "llm.*", "file.*"
	Filters      SubscriptionFilter `json:"filters,omitempty" yaml:"filters,omitempty"`
	Enabled      bool               `json:"enabled" yaml:"enabled"`
	CreatedAt    time.Time          `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	Content      string             // body after frontmatter, sent to LLM on trigger
}

const subscriptionDir = "subscriptions"

func subscriptionPath(name string) string {
	return filepath.Join(subscriptionDir, name+".md")
}

func parseSubscription(data string) (*Subscription, error) {
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

	var sub Subscription
	if err := yaml.Unmarshal([]byte(yamlPart), &sub); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if sub.Name == "" {
		return nil, fmt.Errorf("subscription name is required")
	}
	if sub.EventPattern == "" {
		return nil, fmt.Errorf("event_pattern is required")
	}
	sub.Content = body
	return &sub, nil
}

func serializeSubscription(sub Subscription) (string, error) {
	type front struct {
		Name         string             `yaml:"name"`
		Description  string             `yaml:"description,omitempty"`
		EventPattern string             `yaml:"event_pattern"`
		Filters      SubscriptionFilter `yaml:"filters,omitempty"`
		Enabled      bool               `yaml:"enabled"`
		CreatedAt    time.Time          `yaml:"created_at,omitempty"`
		UpdatedAt    time.Time          `yaml:"updated_at,omitempty"`
	}
	f := front{
		Name:         sub.Name,
		Description:  sub.Description,
		EventPattern: sub.EventPattern,
		Filters:      sub.Filters,
		Enabled:      sub.Enabled,
		CreatedAt:    sub.CreatedAt,
		UpdatedAt:    sub.UpdatedAt,
	}
	yamlData, err := yaml.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("serialize frontmatter: %w", err)
	}
	var sb strings.Builder
	sb.WriteString(frontmatterDelim)
	sb.Write(yamlData)
	sb.WriteString(frontmatterDelim)
	if sub.Content != "" {
		sb.WriteString(sub.Content)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// ReadSubscription reads and parses a subscription from the brain.
func ReadSubscription(ctx context.Context, b *Brain, name string) (*Subscription, error) {
	if name == "" {
		return nil, fmt.Errorf("subscription name is required")
	}
	data, err := b.Read(ctx, subscriptionPath(name))
	if err != nil {
		return nil, err
	}
	return parseSubscription(data)
}

// WriteSubscription serializes and writes a subscription to the brain.
func WriteSubscription(ctx context.Context, b *Brain, sub Subscription) error {
	if sub.Name == "" {
		return fmt.Errorf("subscription name is required")
	}
	now := time.Now()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now
	data, err := serializeSubscription(sub)
	if err != nil {
		return err
	}
	return b.Write(ctx, subscriptionPath(sub.Name), "subscription: "+sub.Name, data)
}

// ListSubscriptions lists all subscriptions stored in the brain.
func ListSubscriptions(ctx context.Context, b *Brain) ([]Subscription, error) {
	files, err := b.List(ctx)
	if err != nil {
		return nil, err
	}

	var subs []Subscription
	prefix := subscriptionDir + "/"
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) || !strings.HasSuffix(f, ".md") {
			continue
		}
		if f == prefix+"index.md" {
			continue
		}
		sub, err := ReadSubscription(ctx, b, strings.TrimSuffix(strings.TrimPrefix(f, prefix), ".md"))
		if err != nil {
			continue // skip unparseable
		}
		subs = append(subs, *sub)
	}
	return subs, nil
}

// DeleteSubscription deletes a subscription from the brain.
func DeleteSubscription(ctx context.Context, b *Brain, name string) error {
	if name == "" {
		return fmt.Errorf("subscription name is required")
	}
	return b.Delete(ctx, subscriptionPath(name))
}
