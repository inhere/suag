package session

import (
	"crypto/rand"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

func newID(prefix string) string {
	prefix = strings.TrimSpace(prefix)

	entropy := ulid.Monotonic(cryptoReader{}, 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy).String()
	if prefix == "" {
		return id
	}
	return prefix + id
}

func NewSessionID() string {
	return newID("sess_")
}

func NewTurnID() string {
	return newID("turn_")
}

func NewRunID() string {
	return newID("run_")
}

func NewEventID() string {
	return newID("evt_")
}

func NewExternalTaskID() string {
	return newID("ext_")
}

func NewRecapID() string {
	return newID("recap_")
}

type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) {
	return io.ReadFull(rand.Reader, p)
}
