package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

const (
	claudeDefaultBaseURL = "https://api.anthropic.com"
	claudeAPIVersion     = "2023-06-01"
)

// ClaudeProvider implements Provider for Anthropic Claude.
type ClaudeProvider struct {
	name         string
	apiKey       string
	model        string
	messagesURL  string
	client       *http.Client
	streamClient *http.Client
	headers      map[string]string
	promptCache  bool
}

// SegmentContextMetaKey is the Message.Metadata key (bool true) that marks the
// segment context message. With PromptCache on, the Claude provider puts a
// cache breakpoint at the end of that message so the stable prefix (system +
// tools + segment context) is cached across turns. The application layer
// must set the same key when it builds that message.
const SegmentContextMetaKey = "suag.segment_context"

// ReasoningFoldedMetaKey is the Message.Metadata key (bool true) that marks an
// assistant message whose reasoning was removed from the request (history
// folding). The OpenAI provider still treats the request as a thinking-mode
// session then, so reasoning_content keeps being echoed on every assistant
// message even when no remaining step carries reasoning.
const ReasoningFoldedMetaKey = "suag.reasoning_folded"

// ClaudeConfig configures a ClaudeProvider.
type ClaudeConfig struct {
	// Name is the provider name reported by Name(); default: anthropic.
	Name   string
	APIKey string
	// BaseURL defaults to https://api.anthropic.com. A URL ending in /v1 gets
	// /messages appended, any other gets /v1/messages.
	BaseURL string
	Model   string // default: claude-sonnet-4-6
	// Headers are extra request headers; see applyHeaders for SessionIDPlaceholder.
	Headers map[string]string
	// PromptCache adds cache_control breakpoints (at most 2 of Anthropic's 4):
	// the segment context message's last block and the request's last block.
	// Default off, since gateways may reject cache_control.
	PromptCache bool
}

// NewClaudeProvider creates a ClaudeProvider.
func NewClaudeProvider(cfg ClaudeConfig) *ClaudeProvider {
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}
	if cfg.Name == "" {
		cfg.Name = "anthropic"
	}
	return &ClaudeProvider{
		name:         cfg.Name,
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		messagesURL:  claudeMessagesURL(cfg.BaseURL),
		client:       &http.Client{Timeout: 120 * time.Second},
		streamClient: newStreamClient(),
		headers:      cfg.Headers,
		promptCache:  cfg.PromptCache,
	}
}

// claudeMessagesURL builds the messages endpoint from a user-supplied base URL.
func claudeMessagesURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = claudeDefaultBaseURL
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

// Name implements Provider.
func (p *ClaudeProvider) Name() string { return p.name }

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

func (p *ClaudeProvider) buildRequestBody(req *agentcoreapi.GenerateRequest, model string) map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages))
	// Anthropic messages only accept user/assistant roles; mid-conversation
	// system messages (rule updates, hook context) go to the top-level system.
	system := req.System
	segmentIdx := -1 // index in msgs of the segment context message, if any
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
				if m.ToolResult.IsError {
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
			if v, _ := m.Metadata[SegmentContextMetaKey].(bool); v {
				segmentIdx = len(msgs)
			}
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
		// A block array only when prompt caching is on: Anthropic accepts both
		// shapes, but some /v1/messages-compatible gateways only take a string.
		if p.promptCache {
			body["system"] = []map[string]any{{"type": "text", "text": system}}
		} else {
			body["system"] = system
		}
	}
	if p.promptCache {
		markClaudeCacheBreakpoints(msgs, segmentIdx)
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

// markClaudeCacheBreakpoints adds cache_control to the last content block of the
// segment context message (segmentIdx, -1 when absent) and of the final
// message. Both pointing at one block yields a single breakpoint. String
// content is converted to a text block first.
func markClaudeCacheBreakpoints(msgs []map[string]any, segmentIdx int) {
	if len(msgs) == 0 {
		return
	}
	last := len(msgs) - 1
	if segmentIdx >= 0 && segmentIdx < last {
		markClaudeLastBlock(msgs[segmentIdx])
	}
	markClaudeLastBlock(msgs[last])
}

func markClaudeLastBlock(msg map[string]any) {
	var blocks []map[string]any
	switch c := msg["content"].(type) {
	case string:
		if c == "" {
			return // Anthropic rejects cache_control on empty text blocks
		}
		blocks = []map[string]any{{"type": "text", "text": c}}
	case []map[string]any:
		blocks = c
	}
	if len(blocks) == 0 {
		return
	}
	blocks[len(blocks)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	msg["content"] = blocks
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
			resp.ToolCalls = append(resp.ToolCalls, newToolCall(block.ID, block.Name, block.Input))
		}
	}
	if raw.StopReason == "max_tokens" {
		markTruncated(resp.ToolCalls)
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.messagesURL, bytes.NewReader(data))
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
