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

type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) {
	return io.ReadFull(rand.Reader, p)
}
