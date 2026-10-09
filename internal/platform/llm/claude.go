package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

const (
	claudeAPIBase    = "https://api.anthropic.com/v1"
	claudeAPIVersion = "2023-06-01"
)

// ClaudeProvider implements Provider for Anthropic Claude.
type ClaudeProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	headers map[string]string
}

// ClaudeConfig configures a ClaudeProvider.
type ClaudeConfig struct {
	APIKey string
	Model  string // default: claude-sonnet-4-6
	// Headers are extra request headers; see applyHeaders for SessionIDPlaceholder.
	Headers map[string]string
}

// NewClaudeProvider creates a ClaudeProvider.
func NewClaudeProvider(cfg ClaudeConfig) *ClaudeProvider {
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}
	return &ClaudeProvider{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: 120 * time.Second},
		headers: cfg.Headers,
	}
}

// Name implements Provider.
func (p *ClaudeProvider) Name() string { return "claude" }

// Generate implements the public agentcore provider contract.
func (p *ClaudeProvider) Generate(ctx context.Context, req *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := p.buildRequestBody(req, model)
	logProviderRequest(p.Name(), "/messages", body)
	respData, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	return p.parseResponse(respData)
}

// Stream implements the public streaming contract. This first iteration keeps
// behavior simple by emitting the final content as a single delta.
func (p *ClaudeProvider) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	if emit != nil && resp != nil && resp.Content != "" {
		if err := emit(ctx, agentcoreapi.StreamEvent{
			Kind:  agentcoreapi.StreamEventTextDelta,
			Delta: resp.Content,
		}); err != nil {
			return nil, err
		}
		if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop}); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// Embed implements Provider — delegates to OpenAI embeddings since Claude has no native embed API.
func (p *ClaudeProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("claude provider does not support embeddings: use an OpenAI-compatible provider for embeddings")
}

// appendClaudeBlock adds a content block as a message of the given role. When
// the previous message has the same role and already holds content blocks
// (parallel tool_use / consecutive tool_result), the block is merged into it so
// roles keep alternating as Anthropic requires.
func appendClaudeBlock(msgs []map[string]any, role string, block map[string]any) []map[string]any {
	if n := len(msgs); n > 0 && msgs[n-1]["role"] == role {
		if blocks, ok := msgs[n-1]["content"].([]map[string]any); ok {
			msgs[n-1]["content"] = append(blocks, block)
			return msgs
		}
	}
	return append(msgs, map[string]any{"role": role, "content": []map[string]any{block}})
}

// toolResultIsError reports whether a tool result is flagged as failed.
// agentcore has no dedicated field, so a Metadata["is_error"]=true marker is honored.
func toolResultIsError(r *agentcoreapi.ToolResult) bool {
	v, _ := r.Metadata["is_error"].(bool)
	return v
}

func (p *ClaudeProvider) buildRequestBody(req *agentcoreapi.GenerateRequest, model string) map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages))
	// Anthropic messages only accept user/assistant roles; mid-conversation
	// system messages (rule updates, hook context) go to the top-level system.
	system := req.System
	for _, m := range req.Messages {
		switch {
		case m.Role == "system" && m.ToolCall == nil && m.ToolResult == nil:
			if m.Content != "" {
				if system != "" {
					system += "\n\n"
				}
				system += m.Content
			}
		// agentcore persists a tool result as role=tool carrying BOTH ToolCall and
		// ToolResult, so the result check must come first; otherwise the result
		// is rendered as a duplicate tool_use and tool_result is never sent.
		case m.ToolResult != nil || m.Role == "tool":
			block := map[string]any{"type": "tool_result", "content": m.Content}
			if m.ToolResult != nil {
				block["tool_use_id"] = m.ToolResult.CallID
				block["content"] = m.ToolResult.Content
				if toolResultIsError(m.ToolResult) {
					block["is_error"] = true
				}
			} else if m.ToolCall != nil {
				block["tool_use_id"] = m.ToolCall.CallID
			}
			msgs = appendClaudeBlock(msgs, "user", block)
		case m.ToolCall != nil:
			// Text the model emitted alongside the call precedes tool_use in the same turn.
			if m.Content != "" {
				msgs = appendClaudeBlock(msgs, "assistant", map[string]any{"type": "text", "text": m.Content})
			}
			msgs = appendClaudeBlock(msgs, "assistant", map[string]any{
				"type":  "tool_use",
				"id":    m.ToolCall.CallID,
				"name":  m.ToolCall.Name,
				"input": m.ToolCall.Arguments,
			})
		default:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
		}
	}

	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": 4096,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if system != "" {
		body["system"] = system
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			}
		}
		body["tools"] = tools
	}

	return body
}

func (p *ClaudeProvider) parseResponse(data []byte) (*agentcoreapi.GenerateResult, error) {
	var raw struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse claude response: %w", err)
	}
	if raw.Error != nil {
		return nil, fmt.Errorf("claude api error: %s", raw.Error.Message)
	}

	resp := &agentcoreapi.GenerateResult{
		Usage: agentcoreapi.Usage{
			InputTokens:      raw.Usage.InputTokens + raw.Usage.CacheReadInputTokens + raw.Usage.CacheCreationInputTokens,
			OutputTokens:     raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.CacheReadInputTokens + raw.Usage.CacheCreationInputTokens + raw.Usage.OutputTokens,
			CacheReadTokens:  raw.Usage.CacheReadInputTokens,
			CacheWriteTokens: raw.Usage.CacheCreationInputTokens,
		},
	}

	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			args, err := decodeToolArguments(block.Input, block.Name)
			if err != nil {
				return nil, err
			}
			resp.ToolCalls = append(resp.ToolCalls, agentcoreapi.ToolCall{
				CallID:    block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	if len(resp.ToolCalls) > 0 {
		// Text emitted alongside the tool calls is kept in resp.Content; the runtime
		// persists it with the first tool-call message.
		resp.FinishReason = agentcoreapi.FinishToolCalls
	} else {
		resp.FinishReason = agentcoreapi.FinishFinal
	}
	return resp, nil
}

func (p *ClaudeProvider) doRequest(ctx context.Context, body map[string]any) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal claude request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIBase+"/messages", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		applyHeaders(ctx, httpReq, p.headers)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", p.apiKey)
		httpReq.Header.Set("anthropic-version", claudeAPIVersion)

		httpResp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		defer httpResp.Body.Close()

		respData, err := io.ReadAll(httpResp.Body)
		if err != nil {
			lastErr = err
			continue
		}
		if httpResp.StatusCode >= 500 {
			logProviderResponse(p.Name(), "/messages", httpResp.StatusCode, respData)
			lastErr = fmt.Errorf("claude server error %d: %s", httpResp.StatusCode, string(respData))
			continue
		}
		logProviderResponse(p.Name(), "/messages", httpResp.StatusCode, respData)
		return respData, nil
	}
	return nil, fmt.Errorf("claude request failed after retries: %w", lastErr)
}
