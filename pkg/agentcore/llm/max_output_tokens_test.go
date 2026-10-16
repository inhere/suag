package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// reqWithLimit builds a one-message request asking for max output tokens (0 =
// no limit configured).
func reqWithLimit(maxTokens int) *agentcoreapi.GenerateRequest {
	return &agentcoreapi.GenerateRequest{
		Messages:  []agentcoreapi.Message{{Role: "user", Content: "hi"}},
		MaxTokens: maxTokens,
	}
}

// openAIBody marshals the chat/completions request one provider would send.
func openAIBody(t *testing.T, p *OpenAIProvider, req *agentcoreapi.GenerateRequest) map[string]any {
	t.Helper()
	data, err := json.Marshal(p.buildRequestBody(req, "m", false))
	assert.NoErr(t, err)
	var out map[string]any
	assert.NoErr(t, json.Unmarshal(data, &out))
	return out
}

// responsesRequestBody marshals the Responses API request one provider would
// send (the name avoids the responsesBody type of the provider itself).
func responsesRequestBody(t *testing.T, p *ResponsesProvider, req *agentcoreapi.GenerateRequest) map[string]any {
	t.Helper()
	return p.buildResponsesBody(context.Background(), req, false)
}

// TestMaxOutputTokensRequestBodies: the limit of each protocol is the
// configured one; without it Claude falls back to its own default while
// OpenAI chat/completions and the Responses API send nothing.
func TestMaxOutputTokensRequestBodies(t *testing.T) {
	t.Run("claude defaults to 32000 without a configured limit", func(t *testing.T) {
		b := marshalBody(t, NewClaudeProvider(ClaudeConfig{}), reqWithLimit(0))
		assert.Eq(t, float64(claudeDefaultMaxOutputTokens), b["max_tokens"])
		assert.Eq(t, 32000, claudeDefaultMaxOutputTokens)
	})
	t.Run("claude sends the configured limit", func(t *testing.T) {
		b := marshalBody(t, NewClaudeProvider(ClaudeConfig{}), reqWithLimit(64000))
		assert.Eq(t, float64(64000), b["max_tokens"])
	})
	t.Run("openai sends only a configured limit", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{})
		assert.Nil(t, openAIBody(t, p, reqWithLimit(0))["max_tokens"])
		assert.Eq(t, float64(8000), openAIBody(t, p, reqWithLimit(8000))["max_tokens"])
	})
	t.Run("responses sends only a configured limit", func(t *testing.T) {
		p := NewResponsesProvider(ResponsesConfig{})
		assert.Nil(t, responsesRequestBody(t, p, reqWithLimit(0))["max_output_tokens"])
		assert.Eq(t, float64(8000), responsesRequestBody(t, p, reqWithLimit(8000))["max_output_tokens"])
		// A compat adapter's own default still fills in an unconfigured limit.
		cc := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1", Model: "deepseek/deepseek-v4.1-flash"}})
		assert.Eq(t, float64(64000), responsesRequestBody(t, cc, reqWithLimit(0))["max_output_tokens"])
	})
	t.Run("claude reports the limit it sent with the answer", func(t *testing.T) {
		var gotBody map[string]any
		server := claudeServer(t, 200, "application/json",
			`{"content":[{"type":"text","text":"half"}],"stop_reason":"max_tokens","usage":{"output_tokens":2}}`, nil, &gotBody, nil)
		res, err := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL}).Generate(context.Background(), reqWithLimit(5000))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, float64(5000), gotBody["max_tokens"])
		assert.True(t, res.Truncated)
		assert.Eq(t, 5000, res.OutputLimit)
	})
}

// TestTruncatedResults: every protocol marks a reply cut off at the output
// limit, also when it carries no tool call to flag.
func TestTruncatedResults(t *testing.T) {
	t.Run("claude stop_reason max_tokens", func(t *testing.T) {
		res, err := NewClaudeProvider(ClaudeConfig{}).parseResponse(
			[]byte(`{"content":[{"type":"text","text":"half"}],"stop_reason":"max_tokens"}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, res.Truncated)
		assert.Eq(t, agentcoreapi.FinishFinal, res.FinishReason)
	})
	t.Run("openai finish_reason length", func(t *testing.T) {
		res, err := NewOpenAIProvider(OpenAIConfig{}).parseResponse(
			[]byte(`{"choices":[{"message":{"content":"half"},"finish_reason":"length"}]}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, res.Truncated)
		assert.Eq(t, agentcoreapi.FinishFinal, res.FinishReason)
	})
	t.Run("responses incomplete for max_output_tokens", func(t *testing.T) {
		res, err := NewResponsesProvider(ResponsesConfig{}).parseResponsesBody(
			[]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"half"}]}]}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, res.Truncated)
	})
	t.Run("finishOf names the cut", func(t *testing.T) {
		assert.Eq(t, finishTruncated, finishOf(&agentcoreapi.GenerateResult{Truncated: true}))
		assert.Eq(t, finishTruncated, finishOf(&agentcoreapi.GenerateResult{
			ToolCalls: []agentcoreapi.ToolCall{{ArgumentsError: truncatedCallError}}}))
		assert.Eq(t, "final", finishOf(&agentcoreapi.GenerateResult{FinishReason: agentcoreapi.FinishFinal}))
	})
}
