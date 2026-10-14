package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// maxStepsServer answers the first two model calls with a tool call
// (suga_manifest: read-only and registered in every mode) and only the third one
// with an answer, so a step limit of 2 forces a wrap-up.
func maxStepsServer(t *testing.T) *httptest.Server {
	t.Helper()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		const usage = `"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}`
		if calls <= 2 {
			_, _ = io.WriteString(w, fmt.Sprintf(
				`{"choices":[{"message":{"tool_calls":[{"id":"call-%d","function":{"name":"suga_manifest","arguments":"{}"}}]},"finish_reason":"tool_calls"}],%s}`,
				calls, usage))
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"partial findings"},"finish_reason":"stop"}],`+usage+`}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// setupMaxStepsRun isolates the config and points the run at the fake provider.
func setupMaxStepsRun(t *testing.T) {
	t.Helper()
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", shortTestHome(t))
	workspace := t.TempDir()
	t.Chdir(workspace)
	assert.NoErr(t, gkconfig.Set("workspace", workspace))
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
	setTestProvider(t, maxStepsServer(t).URL)
}

// streamLines decodes the NDJSON lines of a stream-json run.
func streamLines(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line map[string]any
		assert.NoErr(t, json.Unmarshal(raw, &line))
		lines = append(lines, line)
	}
	return lines
}

// lastStreamLine returns the closing line of a stream-json run (result, error or
// input_required).
func lastStreamLine(t *testing.T, out []byte) map[string]any {
	t.Helper()
	lines := streamLines(t, out)
	assert.Require(t, len(lines) > 0)
	return lines[len(lines)-1]
}

func TestRunMaxStepsFlag(t *testing.T) {
	t.Run("--max-steps is used instead of the config", func(t *testing.T) {
		setupMaxStepsRun(t)
		assert.NoErr(t, gkconfig.Set("max_steps", 100))
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--output-format", "stream-json", "--max-steps", "2"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		last := lastStreamLine(t, out.Bytes())
		assert.Eq(t, "result", last["type"])
		assert.Eq(t, "max_steps", last["wrap_up"])
		assert.Eq(t, "COMPLETED", last["status"])
		assert.Eq(t, "partial findings", last["result"])
	})
	t.Run("a limit above the steps used leaves wrap_up out", func(t *testing.T) {
		setupMaxStepsRun(t)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--output-format", "stream-json", "--max-steps", "5"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		last := lastStreamLine(t, out.Bytes())
		assert.Eq(t, "result", last["type"])
		_, has := last["wrap_up"]
		assert.False(t, has)
	})
	t.Run("text mode names the step limit on stderr only", func(t *testing.T) {
		setupMaxStepsRun(t)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--max-steps", "2"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out.String(), "partial findings")
		assert.StrNotContains(t, out.String(), "步数上限")
		assert.StrContains(t, diag.String(), "步数上限（2 步）收尾")
	})
	t.Run("SUGA_MAX_STEPS is used without the flag", func(t *testing.T) {
		setupMaxStepsRun(t)
		t.Setenv("SUGA_MAX_STEPS", "2")
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--output-format", "stream-json"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "max_steps", lastStreamLine(t, out.Bytes())["wrap_up"])
	})
	t.Run("the config max_steps is used without the flag", func(t *testing.T) {
		setupMaxStepsRun(t)
		assert.NoErr(t, gkconfig.Set("max_steps", 2))
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--output-format", "stream-json"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "max_steps", lastStreamLine(t, out.Bytes())["wrap_up"])
	})
}

func TestRunMaxStepsInvalid(t *testing.T) {
	t.Run("a negative flag is rejected", func(t *testing.T) {
		setupMaxStepsRun(t)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--max-steps", "-2"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "--max-steps must be a whole number >= 1")
	})
	t.Run("a non-numeric flag is rejected", func(t *testing.T) {
		setupMaxStepsRun(t)
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore", "--max-steps", "abc"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "max-steps")
	})
	t.Run("an invalid SUGA_MAX_STEPS is rejected before the run", func(t *testing.T) {
		setupMaxStepsRun(t)
		t.Setenv("SUGA_MAX_STEPS", "0")
		var out, diag bytes.Buffer
		err := RunPersonalCLI(context.Background(), "run",
			[]string{"--prompt", "explore"}, strings.NewReader(""), &out, &diag)
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "SUGA_MAX_STEPS must be a whole number >= 1")
	})
}
