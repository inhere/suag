package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// Stream implements the streaming contract over the Responses API's
// Server-Sent Events: output_text deltas are emitted as they arrive; the
// result comes from the terminal event's response object (or, without one,
// the items of response.output_item.done). A stream that ends without a
// terminal event is a broken stream, requested again by retryStream until
// text was shown.
func (p *ResponsesProvider) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	body := p.buildResponsesBody(ctx, req, true)
	logProviderRequest(p.Name(), responsesPath, body)
	return retryStream(ctx, p.Name(), emit, func(emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
		resp, err := p.openStream(ctx, responsesPath, body)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			// A gateway ignoring stream=true answers with the response object.
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, &streamReadError{fmt.Errorf("read responses result: %w", err)}
			}
			logProviderResponse(p.Name(), responsesPath, http.StatusOK, data)
			res, err := p.parseResponsesBody(data)
			if err == nil {
				err = emitWhole(ctx, emit, res.Content)
			}
			return res, err
		}
		return p.readResponsesStream(ctx, resp.Body, emit)
	})
}

// emitWhole emits a whole text as one delta and a stop.
func emitWhole(ctx context.Context, emit agentcoreapi.StreamFunc, text string) error {
	if emit == nil || text == "" {
		return nil
	}
	if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventTextDelta, Delta: text}); err != nil {
		return err
	}
	return emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop})
}

// responsesEvent is the part of a stream event the provider reads.
type responsesEvent struct {
	Type        string                     `json:"type"`
	Delta       string                     `json:"delta"`
	OutputIndex int                        `json:"output_index"`
	Item        map[string]json.RawMessage `json:"item"`
	Response    *responsesBody             `json:"response"`
	Message     string                     `json:"message"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *ResponsesProvider) readResponsesStream(ctx context.Context, r io.Reader, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	done := map[int]map[string]json.RawMessage{} // output items by index
	emitted := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		payload, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "data:")
		if !ok {
			continue // event: lines, blank separators, comments
		}
		payload = strings.TrimSpace(payload)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev responsesEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil, fmt.Errorf("parse responses stream event: %w", err)
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta == "" || emit == nil {
				continue
			}
			if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventTextDelta, Delta: ev.Delta}); err != nil {
				return nil, err
			}
			emitted = true
		case "response.output_item.done":
			if ev.Item != nil {
				done[ev.OutputIndex] = ev.Item
			}
		case "response.completed", "response.incomplete", "response.done", "response.failed":
			body := responsesBody{}
			if ev.Response != nil {
				body = *ev.Response
			}
			if len(body.Output) == 0 {
				body.Output = orderedItems(done)
			}
			if ev.Type == "response.failed" && (body.Error == nil || body.Error.Message == "") {
				return nil, fmt.Errorf("responses api error: response failed")
			}
			res, err := p.resultOf(body)
			if err != nil {
				return nil, err
			}
			if emitted && emit != nil {
				if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop}); err != nil {
					return nil, err
				}
			}
			return res, nil
		case "error":
			msg := ev.Message
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			return nil, fmt.Errorf("responses api error: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &streamReadError{fmt.Errorf("read responses stream: %w", err)}
	}
	return nil, &streamReadError{fmt.Errorf("responses stream ended without a terminal event")}
}

// orderedItems returns the collected output items by output_index.
func orderedItems(items map[int]map[string]json.RawMessage) []map[string]json.RawMessage {
	keys := make([]int, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]map[string]json.RawMessage, 0, len(keys))
	for _, k := range keys {
		out = append(out, items[k])
	}
	return out
}
