package native

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// WebSearchTool performs web searches via SerpAPI.
// Set SERPAPI_KEY environment variable or pass key directly.
type WebSearchTool struct {
	apiKey string
	client *http.Client
	maxOut int // result cap in bytes; 0 = DefaultMaxOutputBytes
}

// NewWebSearchTool creates a WebSearchTool with the given SerpAPI key.
func NewWebSearchTool(apiKey string) *WebSearchTool {
	return &WebSearchTool{
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *WebSearchTool) Name() string     { return "web_search" }
func (t *WebSearchTool) Kind() tools.Kind { return tools.KindNative }
func (t *WebSearchTool) Description() string {
	return "Search the web using SerpAPI. Returns top organic results."
}
func (t *WebSearchTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query",
				},
				"num": map[string]any{
					"type":        "string",
					"description": "Number of results (default 5)",
				},
			},
			"required": []any{"query"},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	query, _ := call.Arguments["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("web_search: query is required")
	}
	num := "5"
	if n, ok := call.Arguments["num"].(string); ok && n != "" {
		num = n
	}

	params := url.Values{
		"api_key": {t.apiKey},
		"q":       {query},
		"num":     {num},
		"engine":  {"google"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://serpapi.com/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("web_search build request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web_search read response: %w", err)
	}

	content, err := t.formatResults(body)
	if err != nil {
		return nil, err
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: TruncateHeadTail(content, t.maxOut),
	}, nil
}

func (t *WebSearchTool) formatResults(data []byte) (string, error) {
	var result struct {
		OrganicResults []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("web_search parse: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("serpapi error: %s", result.Error)
	}

	var sb strings.Builder
	for i, r := range result.OrganicResults {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.Link, r.Snippet)
	}
	return strings.TrimSpace(sb.String()), nil
}
