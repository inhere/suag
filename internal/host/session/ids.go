package session

import (
	"crypto/rand"
	"io"
	"strconv"
	"sync/atomic"
	"time"
)

var idSequence uint32

func newID(prefix string) string {
	tsPart := strconv.FormatInt(time.Now().UTC().UnixMicro(), 32)
	seqPart := encodeBase32Fixed(uint64(atomic.AddUint32(&idSequence, 1)&1023), 2)
	randPart := shortRandomBase32(2)
	idStr := tsPart + seqPart + randPart

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

func shortRandomBase32(n int) string {
	if n <= 0 {
		return ""
	}

	const alphabet = "0123456789abcdefghijklmnopqrstuv"
	buf := make([]byte, n)
	raw := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		panic(err)
	}
	for i, b := range raw {
		buf[i] = alphabet[int(b)&31]
	}
	return string(buf)
}

func encodeBase32Fixed(value uint64, width int) string {
	if width <= 0 {
		return ""
	}

	const alphabet = "0123456789abcdefghijklmnopqrstuv"
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = alphabet[value&31]
		value >>= 5
	}
	return string(buf)
}
