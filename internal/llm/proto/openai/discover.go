package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dolphin/internal/llm"
)

// DiscoverModels calls the OpenAI-compatible /v1/models endpoint.
func DiscoverModels(ctx context.Context, cfg llm.Config) ([]llm.ModelConfig, error) {
	url := ModelsURL(cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("openai: discover models: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: discover models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: discover models: %s (status %d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai: discover models: decode: %w", err)
	}

	models := make([]llm.ModelConfig, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, llm.ModelConfig{
			Name:    m.ID,
			Model:   m.ID,
			Vendor:  cfg.Vendor,
			APIType: cfg.APIType,
		})
	}
	return models, nil
}
