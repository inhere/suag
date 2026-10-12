package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// streamResponseHeaderTimeout bounds the wait for the response headers of a
// streamed request. The body itself has no deadline (a long answer may stream
// for minutes); ctx cancellation still stops it.
const streamResponseHeaderTimeout = 120 * time.Second

func newStreamClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = streamResponseHeaderTimeout
	return &http.Client{Transport: transport}
}

// Stream implements the public streaming contract with Server-Sent Events:
// text deltas are emitted as they arrive; tool calls, reasoning_content and
// usage are accumulated into the returned result. The connection is retried
// by openStream; a stream that breaks while being read is requested again
// only until it has emitted text (retryStream), so a delta is never emitted
// twice.
func (p *OpenAIProvider) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := p.buildRequestBody(req, model, true)
	body["stream_options"] = map[string]any{"include_usage": true}
	logProviderRequest(p.Name(), "/chat/completions", body)

	return retryStream(ctx, p.Name(), emit, func(emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
		resp, err := p.openStream(ctx, "/chat/completions", body)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			// Some gateways ignore stream=true and answer with a plain completion.
			return p.readWhole(ctx, resp.Body, emit)
		}
		return p.readStream(ctx, resp.Body, emit)
	})
}

// readWhole parses a non-streamed completion and emits its text in one delta.
func (p *OpenAIProvider) readWhole(ctx context.Context, r io.Reader, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, &streamReadError{fmt.Errorf("read openai response: %w", err)}
	}
	logProviderResponse(p.Name(), "/chat/completions", http.StatusOK, data)
	res, err := p.parseResponse(data)
	if err != nil || emit == nil || res.Content == "" {
		return res, err
	}
	if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventTextDelta, Delta: res.Content}); err != nil {
		return nil, err
	}
	return res, emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop})
}

// openStream sends the request and returns a 2xx response whose body is the
// event stream. Network errors and 5xx are retried like doRequest.
func (p *OpenAIProvider) openStream(ctx context.Context, path string, body map[string]any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		applyHeaders(ctx, httpReq, p.headers)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.streamClient.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		logProviderResponse(p.Name(), path, resp.StatusCode, respData)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(respData))
			continue
		}
		// 4xx: report the API's own message, as parseResponse does.
		var apiErr struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respData, &apiErr) == nil && apiErr.Error != nil {
			return nil, fmt.Errorf("openai api error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai api error %d: %s", resp.StatusCode, strings.TrimSpace(string(respData)))
	}
	return nil, fmt.Errorf("openai request failed after retries: %w", lastErr)
}

// streamChunk is one chat.completion.chunk event.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		PromptCacheHitTokens int     `json:"prompt_cache_hit_tokens"` // DeepSeek native
		CostUSD              float64 `json:"cost_usd"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamToolCall accumulates one tool call's fragments by its stream index.
type streamToolCall struct {
	id, name string
	args     strings.Builder
}

// readStream consumes "data: {...}" lines until "data: [DONE]" or EOF.
func (p *OpenAIProvider) readStream(ctx context.Context, r io.Reader, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	var content, reasoning strings.Builder
	calls := map[int]*streamToolCall{}
	res := &agentcoreapi.GenerateResult{}
	emitted := false
	finishReason := "" // the last non-empty finish_reason ("length" = cut off)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue // blank separators, ": keep-alive" comments, event: lines
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		logStreamChunk(p.Name(), payload)
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, fmt.Errorf("parse openai stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("openai api error: %s", chunk.Error.Message)
		}
		if u := chunk.Usage; u != nil {
			res.Usage = agentcoreapi.Usage{
				InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, TotalTokens: u.TotalTokens,
				CacheReadTokens: cachedTokens(u.PromptTokensDetails.CachedTokens, u.PromptCacheHitTokens), CostUSD: u.CostUSD,
			}
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			d := choice.Delta
			reasoning.WriteString(d.ReasoningContent)
			if d.Content != "" {
				content.WriteString(d.Content)
				if emit != nil {
					if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventTextDelta, Delta: d.Content}); err != nil {
						return nil, err
					}
				}
				emitted = true
			}
			for _, tc := range d.ToolCalls {
				call := calls[tc.Index]
				if call == nil {
					call = &streamToolCall{}
					calls[tc.Index] = call
				}
				if tc.ID != "" {
					call.id = tc.ID
				}
				if tc.Function.Name != "" {
					call.name = tc.Function.Name
				}
				call.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &streamReadError{fmt.Errorf("read openai stream: %w", err)}
	}

	res.Content, res.Reasoning = content.String(), reasoning.String()
	indexes := make([]int, 0, len(calls))
	for i := range calls {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		call := calls[i]
		raw := call.args.String()
		if strings.TrimSpace(raw) == "" {
			raw = "{}" // a no-argument call may stream no argument fragment at all
		}
		res.ToolCalls = append(res.ToolCalls, newToolCall(call.id, call.name, json.RawMessage(raw)))
	}
	if finishReason == "length" {
		markTruncated(res.ToolCalls)
	}
	if len(res.ToolCalls) > 0 {
		res.FinishReason = agentcoreapi.FinishToolCalls
	} else {
		res.FinishReason = agentcoreapi.FinishFinal
	}
	if emitted && emit != nil {
		if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop}); err != nil {
			return nil, err
		}
	}
	return res, nil
}
