package session

import (
	"crypto/rand"
	"io"
	"strconv"
	"time"
)

func newID(prefix string) string {
	tsInt := time.Now().UnixMicro()
	// convert tsInt to base32 and remove padding
	tsStr := strconv.FormatInt(tsInt, 32)

	if prefix == "" {
		return tsStr
	}
	return prefix + tsStr
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
