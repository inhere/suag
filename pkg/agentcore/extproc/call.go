// Package extproc runs external helper processes (exec backends) over the
// suag exec protocol v1 and provides the credential-stripped environment
// shared with MCP subprocesses.
//
// Protocol v1: one process per call. The caller writes ONE line of JSON to
// the child's stdin:
//
//	{"v":1,"capability":"memory","op":"search","args":{...}}
//
// and the child answers with ONE line of JSON on stdout:
//
//	{"ok":true,"result":{...}}   or   {"ok":false,"error":"..."}
//
// A non-zero exit, a timeout or output that is not valid JSON is a failure;
// the error carries the tail of the child's stderr. Request and Response are
// the envelope types; Call itself is envelope-agnostic and decodes the stdout
// line into whatever resp it is given (normally *Response).
package extproc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// ProtocolVersion is the value of Request.V.
	ProtocolVersion = 1
	// DefaultTimeout bounds a call when Spec.Timeout is zero.
	DefaultTimeout = 10 * time.Second

	stderrTailBytes = 2048
	maxStdoutBytes  = 4 << 20
)

// Request is the protocol v1 request envelope.
type Request struct {
	V          int    `json:"v"`
	Capability string `json:"capability"`
	Op         string `json:"op"`
	Args       any    `json:"args,omitempty"`
}

// Response is the protocol v1 response envelope.
type Response struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Spec describes the child process of one call.
type Spec struct {
	Command string
	Args    []string
	// Env holds extra entries appended to the credential-stripped parent
	// environment (StripCredentials(os.Environ(), Env)); empty means just the
	// stripped parent environment.
	Env []string
	// Timeout bounds the whole call; zero means DefaultTimeout.
	Timeout time.Duration
}

// Call runs spec.Command, writes req as one JSON line to its stdin, and
// decodes the first non-empty stdout line into resp.
func Call(ctx context.Context, spec Spec, req any, resp any) error {
	if spec.Command == "" {
		return errors.New("extproc: empty command")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("extproc: encode request: %w", err)
	}
	payload = append(payload, '\n')

	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Env = StripCredentials(os.Environ(), spec.Env)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout limitedBuffer
	stdout.limit = maxStdoutBytes
	var stderr tailBuffer
	stderr.limit = stderrTailBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Without WaitDelay a grandchild holding the pipes would block Wait after the kill.
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(ctxErr, context.DeadlineExceeded) {
		return fmt.Errorf("extproc: %s timed out after %s%s", spec.Command, timeout, stderr.suffix())
	} else if ctxErr != nil {
		return fmt.Errorf("extproc: %s canceled: %w%s", spec.Command, ctxErr, stderr.suffix())
	}
	if runErr != nil {
		return fmt.Errorf("extproc: %s failed: %w%s", spec.Command, runErr, stderr.suffix())
	}
	if stdout.over {
		return fmt.Errorf("extproc: %s output exceeds %d bytes%s", spec.Command, maxStdoutBytes, stderr.suffix())
	}

	line := firstLine(stdout.buf.Bytes())
	if len(line) == 0 {
		return fmt.Errorf("extproc: %s produced no output%s", spec.Command, stderr.suffix())
	}
	if err := json.Unmarshal(line, resp); err != nil {
		return fmt.Errorf("extproc: %s returned invalid JSON: %w%s", spec.Command, err, stderr.suffix())
	}
	return nil
}

// firstLine returns the first non-blank line of b.
func firstLine(b []byte) []byte {
	for _, l := range bytes.Split(b, []byte("\n")) {
		if l = bytes.TrimSpace(l); len(l) > 0 {
			return l
		}
	}
	return nil
}

// limitedBuffer collects stdout up to limit bytes and flags overflow instead
// of growing without bound.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.over = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

// tailBuffer keeps only the last limit bytes written to it.
type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

// suffix formats the stderr tail for appending to an error message.
func (b *tailBuffer) suffix() string {
	s := strings.TrimSpace(string(b.data))
	if s == "" {
		return ""
	}
	return ": stderr: " + s
}
