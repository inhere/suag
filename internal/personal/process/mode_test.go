package process

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// Tests of the wire side of Request.Mode and Response.Steered: a client that
// predates the field (no "mode" in its request) must keep the follow-up
// behaviour, and a "steer" request must be told when its message reached the
// turn running right now.

// rawExchange opens the session socket, writes one raw JSON request and reads the
// responses that follow: the first one right away, further ones (the fate of a
// message the sender waited for) within extra of it. It returns what arrived.
func rawExchange(t *testing.T, record Record, request string, extra time.Duration) []Response {
	t.Helper()
	conn, err := net.DialTimeout("unix", record.Socket, 2*time.Second)
	assert.Require(t, assert.NoErr(t, err))
	defer conn.Close()
	_, err = conn.Write([]byte(request))
	assert.Require(t, assert.NoErr(t, err))

	decoder := json.NewDecoder(bufio.NewReader(conn))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var out []Response
	var first Response
	if err := decoder.Decode(&first); err != nil {
		return out
	}
	out = append(out, first)
	_ = conn.SetReadDeadline(time.Now().Add(extra))
	var next Response
	if err := decoder.Decode(&next); err == nil {
		out = append(out, next)
	}
	return out
}

// modeServer starts a session socket whose deliver func answers answer and hands
// the mode it was asked for to modes.
func modeServer(t *testing.T, modes chan<- string, answer func() (Delivery, <-chan error, error)) Record {
	t.Helper()
	server, err := StartWait(shortProcessRoot(t), "session-mode", "mode", t.TempDir(),
		func(mode, text, from string) (Delivery, <-chan error, error) {
			modes <- mode
			return answer()
		})
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = server.Close() })
	return server.Record()
}

func TestDeliverRequestModeAndSteeredAck(t *testing.T) {
	t.Run("a request without the field is a follow-up", func(t *testing.T) {
		modes := make(chan string, 4)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			return Delivery{Queued: true}, nil, nil
		})
		// The exact body an older client sends: no "mode" key at all.
		body := `{"type":"deliver","session_id":"session-mode","text":"hi","from":"cli"}`
		responses := rawExchange(t, record, body, 200*time.Millisecond)
		assert.Require(t, assert.Len(t, responses, 1))
		assert.True(t, responses[0].OK)
		assert.True(t, responses[0].Queued)
		assert.False(t, responses[0].Steered)
		// The response carries no steered field: an older client decodes it as before.
		raw, err := json.Marshal(responses[0])
		assert.Require(t, assert.NoErr(t, err))
		assert.StrNotContains(t, string(raw), "steered")
		assert.Eq(t, ModeFollowUp, <-modes)
	})

	t.Run("an explicit follow_up mode is the same", func(t *testing.T) {
		modes := make(chan string, 4)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			return Delivery{Queued: false}, nil, nil
		})
		body := `{"type":"deliver","session_id":"session-mode","text":"hi","mode":"follow_up"}`
		responses := rawExchange(t, record, body, 200*time.Millisecond)
		assert.Require(t, assert.Len(t, responses, 1))
		assert.True(t, responses[0].OK)
		assert.False(t, responses[0].Queued)
		assert.Eq(t, ModeFollowUp, <-modes)
	})

	t.Run("a steer without wait is acknowledged and nothing more", func(t *testing.T) {
		modes := make(chan string, 4)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			// The channel is never written: the fate of the message is unknown.
			return Delivery{Steered: true}, make(chan error, 1), nil
		})
		body := `{"type":"deliver","session_id":"session-mode","text":"hi","mode":"steer"}`
		responses := rawExchange(t, record, body, 200*time.Millisecond)
		assert.Require(t, assert.Len(t, responses, 1))
		assert.True(t, responses[0].Steered)
		assert.False(t, responses[0].Queued)
		assert.Eq(t, ModeSteer, <-modes)
	})

	t.Run("a steer with wait reports the message reaching the model", func(t *testing.T) {
		modes := make(chan string, 4)
		settled := make(chan error, 1)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			return Delivery{Steered: true}, settled, nil
		})
		go func() {
			time.Sleep(50 * time.Millisecond)
			settled <- nil
		}()
		body := `{"type":"deliver","session_id":"session-mode","text":"hi","mode":"steer","wait":true}`
		responses := rawExchange(t, record, body, time.Second)
		assert.Require(t, assert.Len(t, responses, 2))
		assert.True(t, responses[0].Steered)
		assert.True(t, responses[1].OK)
		assert.True(t, responses[1].Steered)
		assert.Eq(t, ModeSteer, <-modes)
	})

	t.Run("a discarded steer ends in code discarded", func(t *testing.T) {
		modes := make(chan string, 4)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			// A message that will never run: its sender is told at once.
			settled := make(chan error, 1)
			settled <- ErrDiscarded
			return Delivery{Steered: true}, settled, nil
		})
		body := `{"type":"deliver","session_id":"session-mode","text":"hi","mode":"steer","wait":true}`
		responses := rawExchange(t, record, body, time.Second)
		assert.Require(t, assert.Len(t, responses, 2))
		assert.Eq(t, codeDiscarded, responses[1].Error)
		assert.True(t, responses[1].Steered)
		// What the client (`suga send`) turns into exit code 3.
		response, err := Send(record, Request{Text: "hi", Mode: ModeSteer, Wait: true})
		assert.True(t, err != nil)
		assert.True(t, errors.Is(err, ErrNotOnline))
		assert.Eq(t, codeDiscarded, response.Error)
	})

	t.Run("a full inbox is answered queue_full", func(t *testing.T) {
		modes := make(chan string, 4)
		record := modeServer(t, modes, func() (Delivery, <-chan error, error) {
			return Delivery{}, nil, ErrQueueFull
		})
		response, err := Send(record, Request{Text: "hi", Mode: ModeSteer})
		assert.True(t, err != nil)
		assert.True(t, errors.Is(err, ErrQueueFull))
		assert.Eq(t, "queue_full", response.Error)
		assert.Eq(t, ModeSteer, <-modes)
	})
}

// TestResponseDecodesWithoutSteered: a response written by an older server (no
// steered field) still decodes, and a client sends its request the way it always
// did when it does not ask for steering.
func TestResponseDecodesWithoutSteered(t *testing.T) {
	var response Response
	assert.Require(t, assert.NoErr(t, json.Unmarshal([]byte(`{"ok":true,"queued":true}`), &response)))
	assert.True(t, response.OK)
	assert.True(t, response.Queued)
	assert.False(t, response.Steered)

	raw, err := json.Marshal(Request{Text: "hi", From: "cli"})
	assert.Require(t, assert.NoErr(t, err))
	assert.StrNotContains(t, string(raw), "mode")
}
