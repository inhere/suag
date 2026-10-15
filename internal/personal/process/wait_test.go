package process

import (
	"errors"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestSendWait_QueuedFate(t *testing.T) {
	newServer := func(t *testing.T, settled chan error) Record {
		t.Helper()
		root := shortProcessRoot(t)
		server, err := StartWait(root, "session-w1", "wait", t.TempDir(), func(mode, text, from string) (Delivery, <-chan error, error) {
			return Delivery{Queued: true}, settled, nil
		})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = server.Close() })
		return server.Record()
	}
	type result struct {
		resp Response
		err  error
	}
	send := func(record Record, wait bool) chan result {
		ch := make(chan result, 1)
		go func() {
			resp, err := Send(record, Request{Text: "hi", From: "cli", Wait: wait})
			ch <- result{resp, err}
		}()
		return ch
	}

	t.Run("discarded queued message ends in ErrNotOnline", func(t *testing.T) {
		settled := make(chan error, 1)
		ch := send(newServer(t, settled), true)
		select {
		case <-ch:
			t.Fatal("Send returned before the queued message was settled")
		case <-time.After(150 * time.Millisecond):
		}
		settled <- ErrDiscarded
		res := <-ch
		assert.True(t, errors.Is(res.err, ErrNotOnline))
	})

	t.Run("consumed queued message succeeds", func(t *testing.T) {
		settled := make(chan error, 1)
		settled <- nil
		res := <-send(newServer(t, settled), true)
		assert.NoErr(t, res.err)
		assert.True(t, res.resp.OK)
	})

	t.Run("server close fails a waiting sender", func(t *testing.T) {
		settled := make(chan error, 1)
		root := shortProcessRoot(t)
		server, err := StartWait(root, "session-w2", "wait", t.TempDir(), func(mode, text, from string) (Delivery, <-chan error, error) {
			return Delivery{Queued: true}, settled, nil
		})
		assert.Require(t, assert.NoErr(t, err))
		ch := send(server.Record(), true)
		time.Sleep(150 * time.Millisecond)
		_ = server.Close()
		res := <-ch
		assert.True(t, errors.Is(res.err, ErrNotOnline))
	})

	t.Run("without Wait the sender returns on the queued ack", func(t *testing.T) {
		res := <-send(newServer(t, make(chan error, 1)), false)
		assert.NoErr(t, res.err)
		assert.True(t, res.resp.Queued)
	})

	t.Run("closed session refuses delivery as not online", func(t *testing.T) {
		root := shortProcessRoot(t)
		server, err := StartWait(root, "session-w3", "wait", t.TempDir(), func(mode, text, from string) (Delivery, <-chan error, error) {
			return Delivery{}, nil, ErrSessionClosed
		})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = server.Close() })
		_, err = Send(server.Record(), Request{Text: "hi"})
		assert.True(t, errors.Is(err, ErrNotOnline))
	})
}
