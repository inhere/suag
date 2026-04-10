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

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
)

// WebSearchTool performs web searches via SerpAPI.
// Set SERPAPI_KEY environment variable or pass key directly.
type WebSearchTool struct {
	apiKey string
	client *http.Client
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
func (t *WebSearchTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"query": {Type: "string", Description: "Search query"},
				"num":   {Type: "string", Description: "Number of results (default 5)"},
			},
			Required: []string{"query"},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("web_search: query is required")
	}
	num := "5"
	if n, ok := args["num"].(string); ok && n != "" {
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
		return "", fmt.Errorf("web_search build request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("web_search read response: %w", err)
	}

	return t.formatResults(body)
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
