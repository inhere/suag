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

const defaultOpenAIBase = "https://api.openai.com/v1"

// OpenAIProvider implements Provider for OpenAI-compatible APIs
// (OpenAI, DeepSeek, Ollama, etc.).
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	embedModel string
	client     *http.Client
	name       string
}

// OpenAIConfig configures an OpenAIProvider.
type OpenAIConfig struct {
	APIKey     string
	BaseURL    string // default: https://api.openai.com/v1
	Model      string // default: gpt-4o
	EmbedModel string // default: text-embedding-3-small
	Name       string // default: "openai"
}

// NewOpenAIProvider creates an OpenAIProvider from cfg.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultOpenAIBase
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}
	if cfg.EmbedModel == "" {
		cfg.EmbedModel = "text-embedding-3-small"
	}
	if cfg.Name == "" {
		cfg.Name = "openai"
	}
	return &OpenAIProvider{
		apiKey:     cfg.APIKey,
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		embedModel: cfg.EmbedModel,
		client:     &http.Client{Timeout: 120 * time.Second},
		name:       cfg.Name,
	}
}

// Name implements Provider.
func (p *OpenAIProvider) Name() string { return p.name }

// Generate implements the public agentcore provider contract.
func (p *OpenAIProvider) Generate(ctx context.Context, req *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := p.buildRequestBody(req, model, false)
	logProviderRequest(p.Name(), "/chat/completions", body)
	respBody, err := p.doRequest(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	return p.parseResponse(respBody)
}

// Stream implements the public streaming contract. This first iteration keeps
// behavior simple by emitting the final content as a single delta.
func (p *OpenAIProvider) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
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

// Embed implements Provider.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"model": p.embedModel,
		"input": text,
	}
	logProviderRequest(p.Name(), "/embeddings", body)
	respBody, err := p.doRequest(ctx, "/embeddings", body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return result.Data[0].Embedding, nil
}

// buildRequestBody constructs the OpenAI chat completion request payload.
func (p *OpenAIProvider) buildRequestBody(req *agentcoreapi.GenerateRequest, model string, stream bool) map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)

	// System prompt injection.
	system := req.System
	if system != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": system})
	}

	for _, m := range req.Messages {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		if m.ToolCall != nil {
			msg["tool_calls"] = []map[string]any{{
				"id":   m.ToolCall.CallID,
				"type": "function",
				"function": map[string]any{
					"name":      m.ToolCall.Name,
					"arguments": mustJSON(m.ToolCall.Arguments),
				},
			}}
			msg["content"] = nil
		}
		if m.ToolResult != nil {
			msg["role"] = "tool"
			msg["tool_call_id"] = m.ToolResult.CallID
			msg["content"] = m.ToolResult.Content
		}
		msgs = append(msgs, msg)
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.InputSchema,
				},
			}
		}
		body["tools"] = tools
	}
	return body
}

// parseResponse decodes an OpenAI chat completion response.
func (p *OpenAIProvider) parseResponse(data []byte) (*agentcoreapi.GenerateResult, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CostUSD float64 `json:"cost_usd"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	if raw.Error != nil {
		return nil, fmt.Errorf("openai api error: %s", raw.Error.Message)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty choices")
	}

	choice := raw.Choices[0]
	resp := &agentcoreapi.GenerateResult{
		Content: choice.Message.Content,
		Usage: agentcoreapi.Usage{
			InputTokens:     raw.Usage.PromptTokens,
			OutputTokens:    raw.Usage.CompletionTokens,
			TotalTokens:     raw.Usage.TotalTokens,
			CacheReadTokens: raw.Usage.PromptTokensDetails.CachedTokens,
			CostUSD:         raw.Usage.CostUSD,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		args, err := decodeToolArguments(tc.Function.Arguments, tc.Function.Name)
		if err != nil {
			return nil, err
		}
		resp.ToolCalls = append(resp.ToolCalls, agentcoreapi.ToolCall{
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	if len(resp.ToolCalls) > 0 {
		// Some OpenAI-compatible providers emit a short assistant preamble
		// alongside tool calls. The public agentcore contract models a step as
		// either final text or tool requests, so prefer the actionable tool calls.
		resp.Content = ""
		resp.FinishReason = agentcoreapi.FinishToolCalls
	} else {
		resp.FinishReason = agentcoreapi.FinishFinal
	}

	return resp, nil
}

// doRequest sends a POST to the OpenAI API and returns the raw response body.
func (p *OpenAIProvider) doRequest(ctx context.Context, path string, body map[string]any) ([]byte, error) {
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
			return nil, fmt.Errorf("marshal request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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
			logProviderResponse(p.Name(), path, httpResp.StatusCode, respData)
			lastErr = fmt.Errorf("server error %d: %s", httpResp.StatusCode, string(respData))
			continue
		}

		logProviderResponse(p.Name(), path, httpResp.StatusCode, respData)
		return respData, nil
	}
	return nil, fmt.Errorf("openai request failed after retries: %w", lastErr)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
