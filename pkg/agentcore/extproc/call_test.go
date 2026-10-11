package extproc

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// helperEnv selects the helper-process behaviour; TestMain turns the test
// binary into a fake external backend when it is set.
const helperEnv = "EXTPROC_TEST_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "echo":
		// Answers with the capability/op it received, proving stdin was delivered.
		var in Request
		_ = jsonDecodeLine(os.Stdin, &in)
		os.Stdout.WriteString(`{"ok":true,"result":{"cap":"` + in.Capability + `","op":"` + in.Op + `"}}` + "\n")
	case "sleep":
		time.Sleep(30 * time.Second)
	case "badjson":
		os.Stdout.WriteString("not json at all\n")
	case "fail":
		os.Stderr.WriteString("boom: backend exploded\n")
		os.Exit(3)
	case "envreport":
		// Reports whether the credential variable leaked into the child.
		_, leaked := os.LookupEnv("EXTPROC_TEST_API_KEY")
		if leaked {
			os.Stdout.WriteString(`{"ok":true,"result":{"leaked":true}}` + "\n")
		} else {
			os.Stdout.WriteString(`{"ok":true,"result":{"leaked":false}}` + "\n")
		}
	}
	os.Exit(0)
}

func helperSpec(mode string, extraEnv ...string) Spec {
	return Spec{Command: os.Args[0], Env: append([]string{helperEnv + "=" + mode}, extraEnv...), Timeout: 5 * time.Second}
}

func TestCall(t *testing.T) {
	req := Request{V: ProtocolVersion, Capability: "memory", Op: "search", Args: map[string]any{"q": "x"}}

	t.Run("success", func(t *testing.T) {
		var resp Response
		assert.NoErr(t, Call(context.Background(), helperSpec("echo"), req, &resp))
		assert.True(t, resp.OK)
		assert.Eq(t, `{"cap":"memory","op":"search"}`, string(resp.Result))
	})

	t.Run("timeout", func(t *testing.T) {
		spec := helperSpec("sleep")
		spec.Timeout = 200 * time.Millisecond
		start := time.Now()
		var resp Response
		err := Call(context.Background(), spec, req, &resp)
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "timed out")
		assert.True(t, time.Since(start) < 10*time.Second)
	})

	t.Run("invalid output", func(t *testing.T) {
		var resp Response
		err := Call(context.Background(), helperSpec("badjson"), req, &resp)
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "invalid JSON")
	})

	t.Run("non-zero exit carries stderr", func(t *testing.T) {
		var resp Response
		err := Call(context.Background(), helperSpec("fail"), req, &resp)
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "boom: backend exploded")
	})

	t.Run("credentials stripped from inherited env", func(t *testing.T) {
		t.Setenv("EXTPROC_TEST_API_KEY", "secret-value")
		var resp Response
		assert.NoErr(t, Call(context.Background(), helperSpec("envreport"), req, &resp))
		assert.Eq(t, `{"leaked":false}`, string(resp.Result))

		// An explicitly configured credential is still passed through.
		resp = Response{}
		spec := helperSpec("envreport", "EXTPROC_TEST_API_KEY=explicit")
		assert.NoErr(t, Call(context.Background(), spec, req, &resp))
		assert.Eq(t, `{"leaked":true}`, string(resp.Result))
	})

	t.Run("empty command", func(t *testing.T) {
		assert.Err(t, Call(context.Background(), Spec{}, req, new(Response)))
	})

	t.Run("stderr tail is bounded", func(t *testing.T) {
		var tb tailBuffer
		tb.limit = 8
		_, _ = tb.Write([]byte(strings.Repeat("a", 20) + "END"))
		assert.Eq(t, ": stderr: aaaaaEND", tb.suffix())
	})
}

// jsonDecodeLine decodes the first JSON value from r (helper-process side).
func jsonDecodeLine(r *os.File, v any) error {
	return json.NewDecoder(r).Decode(v)
}
