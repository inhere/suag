package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// outputLimitServer answers every model call with an answer cut off at the
// output limit (OpenAI finish_reason "length") and records the request bodies.
func outputLimitServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"half an answer"},"finish_reason":"length"}],`+
			`"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// setOutputLimitProvider points the openai preset at baseURL and configures the
// output limit: 16000 for the provider, 8000 for gpt-4o.
func setOutputLimitProvider(t *testing.T, baseURL string) {
	t.Helper()
	assert.NoErr(t, gkconfig.Set("providers", map[string]any{"openai": map[string]any{
		"type": "openai", "api_key": "local-test-key", "base_url": baseURL,
		"max_output_tokens": 16000,
		"models":            map[string]any{"gpt-4o": map[string]any{"max_output_tokens": 8000}},
	}}))
}

// TestRunOutputLimitNotice: an answer the model cut off at the output limit is
// reported on stderr (text mode) and in the closing stream-json line, and the
// configured limit reaches the request body.
func TestRunOutputLimitNotice(t *testing.T) {
	t.Run("text mode names the limit and the request carries it", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var bodies []map[string]any
		setOutputLimitProvider(t, outputLimitServer(t, &bodies).URL)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "half an answer\n", out.String())
		assert.StrContains(t, diag.String(), "输出上限（8000 tokens）")
		assert.StrContains(t, diag.String(), "--max-output-tokens")
		assert.Require(t, assert.Len(t, bodies, 1))
		assert.Eq(t, float64(8000), bodies[0]["max_tokens"])
	})
	t.Run("stream-json marks the result line", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var bodies []map[string]any
		setOutputLimitProvider(t, outputLimitServer(t, &bodies).URL)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--output-format", "stream-json"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		line := lastStreamLine(t, out.Bytes())
		assert.Eq(t, "result", line["type"])
		assert.Eq(t, true, line["truncated"])
		assert.Eq(t, float64(8000), line["output_limit"])
	})
	t.Run("--max-output-tokens overrides the configuration", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var bodies []map[string]any
		setOutputLimitProvider(t, outputLimitServer(t, &bodies).URL)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--max-output-tokens", "4000"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, bodies, 1))
		assert.Eq(t, float64(4000), bodies[0]["max_tokens"])
		assert.StrContains(t, diag.String(), "输出上限（4000 tokens）")
	})
	t.Run("SUGA_MAX_OUTPUT_TOKENS overrides the configuration", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var bodies []map[string]any
		setOutputLimitProvider(t, outputLimitServer(t, &bodies).URL)
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "2000")
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, bodies, 1))
		assert.Eq(t, float64(2000), bodies[0]["max_tokens"])
	})
	t.Run("without any configuration no limit is sent and the cut is named without one", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var bodies []map[string]any
		setTestProvider(t, outputLimitServer(t, &bodies).URL)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, bodies, 1))
		assert.Nil(t, bodies[0]["max_tokens"])
		assert.StrContains(t, diag.String(), "本轮回答达到输出上限而被截断")
	})
	t.Run("a value below 1 is rejected before the run", func(t *testing.T) {
		setupMaxStepsConfig(t)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--max-output-tokens", "-1"}, strings.NewReader(""), &out, &diag)
		assert.ErrMsgContains(t, err, "--max-output-tokens must be a whole number >= 1")
	})
	t.Run("an invalid SUGA_MAX_OUTPUT_TOKENS is rejected before the run", func(t *testing.T) {
		setupMaxStepsConfig(t)
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "abc")
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore"}, strings.NewReader(""), &out, &diag)
		assert.ErrMsgContains(t, err, "SUGA_MAX_OUTPUT_TOKENS must be a whole number >= 1")
	})
}
