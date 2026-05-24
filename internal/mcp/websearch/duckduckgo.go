package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	registerProvider("duckduckgo", func(w *Tool, ctx context.Context, query string) ([]searchResult, error) {
		return w.searchDuckDuckGo(ctx, query)
	})
}

func (w *Tool) searchDuckDuckGo(ctx context.Context, query string) ([]searchResult, error) {
	baseURL := "https://html.duckduckgo.com/html/"
	if u, ok := w.cfg.ProviderBaseURLs["duckduckgo"]; ok && u != "" {
		baseURL = u
	}
	u := fmt.Sprintf("%s?q=%s", baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	ua := "Mozilla/5.0 (compatible; DolphinAgent/1.0)"
	if w.cfg.UserAgent != "" {
		ua = w.cfg.UserAgent
	}
	req.Header.Set("User-Agent", ua)

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	maxResults := w.cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	return parseDuckDuckGoHTML(string(body), maxResults), nil
}

func parseDuckDuckGoHTML(html string, maxResults int) []searchResult {
	var results []searchResult
	linkStart := `<a rel="nofollow" class="result__a" href="`
	linkEnd := `</a>`
	snippetClass := `class="result__snippet">`
	snippetEnd := `</a>`

	remaining := html
	for {
		idx := strings.Index(remaining, linkStart)
		if idx < 0 {
			break
		}
		remaining = remaining[idx+len(linkStart):]

		quoteIdx := strings.IndexByte(remaining, '"')
		if quoteIdx < 0 {
			continue
		}
		resultURL := remaining[:quoteIdx]
		resultURL = strings.ReplaceAll(resultURL, "&amp;", "&")

		titleTag := `">`
		titleIdx := strings.Index(remaining, titleTag)
		if titleIdx < 0 {
			continue
		}
		titlePart := remaining[titleIdx+len(titleTag):]
		endIdx := strings.Index(titlePart, linkEnd)
		if endIdx < 0 {
			continue
		}
		title := titlePart[:endIdx]

		remaining = titlePart[endIdx+len(linkEnd):]

		snipIdx := strings.Index(remaining, snippetClass)
		snippet := ""
		if snipIdx >= 0 {
			snipPart := remaining[snipIdx+len(snippetClass):]
			snipEnd := strings.Index(snipPart, snippetEnd)
			if snipEnd >= 0 {
				snippet = snipPart[:snipEnd]
			}
		}

		results = append(results, searchResult{
			Title:   unescapeHTML(title),
			URL:     resultURL,
			Snippet: unescapeHTML(snippet),
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

func unescapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}
