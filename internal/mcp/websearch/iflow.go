package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	registerProvider("iflow", func(w *Tool, ctx context.Context, query string) ([]searchResult, error) {
		return w.searchIflow(ctx, query)
	})
}

func (w *Tool) searchIflow(ctx context.Context, query string) ([]searchResult, error) {
	if w.cfg.APIKey == "" {
		return nil, fmt.Errorf("iflow provider requires api_key (set mcp.web_search.api_key)")
	}

	payload, _ := json.Marshal(map[string]any{
		"keywords": query,
		"num":      5,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://platform.iflow.cn/api/search/webSearch", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iflow request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read iflow response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("iflow API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var bizResp struct {
		Success bool            `json:"success"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &bizResp); err != nil {
		return nil, fmt.Errorf("parse iflow response: %w", err)
	}
	if !bizResp.Success {
		return nil, fmt.Errorf("iflow API error: %s (code: %s)", bizResp.Message, bizResp.Code)
	}

	var data struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.Unmarshal(bizResp.Data, &data); err != nil {
		return nil, fmt.Errorf("parse iflow data: %w", err)
	}

	var results []searchResult
	for _, r := range data.Organic {
		results = append(results, searchResult{
			Title:   r.Title,
			URL:     r.Link,
			Snippet: r.Snippet,
		})
	}
	return results, nil
}
