package session

import (
	"crypto/rand"
	"io"
	"time"

	"github.com/oklog/ulid/v2"
)

func newID(prefix string) string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), cryptoReader{})
	idStr := id.String()

	if prefix == "" {
		return idStr
	}
	return prefix + idStr
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

func NewMemoryCandidateID() string {
	return newID("mcand_")
}

type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) {
	return io.ReadFull(rand.Reader, p)
}
