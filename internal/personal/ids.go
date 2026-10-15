package personal

import (
	"crypto/rand"
	"io"
	"strconv"
	"sync/atomic"
	"time"
)

var idSequence uint32

// newID builds a time-ordered, prefixed id (same shape the former host session
// package produced: micro timestamp + sequence + random suffix).
func newID(prefix string) string {
	ts := strconv.FormatInt(time.Now().UTC().UnixMicro(), 32)
	seq := encodeBase32Fixed(uint64(atomic.AddUint32(&idSequence, 1)&32767), 3)
	return prefix + ts + seq + randomBase32(1)
}

// NewSessionID returns a fresh session id: "sess_" + local yymmdd + "_" + the
// time-ordered suffix, e.g. sess_261014_1itbudb7rum0012, so the creation day is
// readable at a glance and the id stays sortable by creation time.
func NewSessionID() string { return newID("sess_" + time.Now().Format("060102") + "_") }

// NewTurnID returns a fresh turn id ("turn_..."); it doubles as the run id.
func NewTurnID() string { return newID("turn_") }

const base32Alphabet = "0123456789abcdefghijklmnopqrstuv"

func randomBase32(n int) string {
	raw := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		panic(err)
	}
	buf := make([]byte, n)
	for i, b := range raw {
		buf[i] = base32Alphabet[int(b)&31]
	}
	return string(buf)
}

func encodeBase32Fixed(value uint64, width int) string {
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = base32Alphabet[value&31]
		value >>= 5
	}
	return string(buf)
}
