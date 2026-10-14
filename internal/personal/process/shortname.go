package process

import (
	crand "crypto/rand"
	"path/filepath"
	"strings"
	"time"
)

// A chat process registers under a short name — "<workspace directory>-<xy>",
// like "suga-k7" — so it can be addressed as an address instead of a session ID:
// `suga send --session suga-k7 你好`. The name is generated when the session
// first registers its local session socket, from the directory chat runs in plus
// a short random suffix, and is unique among the live chat processes of one
// process root (the SUGA_HOME that `suga ps` and `suga send` read).
const (
	// shortNameMaxDirLen caps the directory part of a short name in characters.
	shortNameMaxDirLen = 20
	// shortNameSuffixLen is the length of the random suffix: 36² = 1296 names
	// per directory.
	shortNameSuffixLen = 2
	// shortNameAttempts is how many suffixes are tried before the generator stops
	// insisting on uniqueness and returns a longer name anyway (see NewShortName).
	shortNameAttempts = 32
	// shortNameAlphabet is both the suffix alphabet and the only characters the
	// directory part keeps, so a name never needs quoting in a shell.
	shortNameAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	// shortNameFallback is the directory part when nothing usable is left, e.g. a
	// workspace directory named "中文" or "_": a name is never just "-k7".
	shortNameFallback = "suga"
)

// SuffixSource returns one random name suffix of shortNameSuffixLen characters.
// It is a parameter so tests can inject a deterministic source.
type SuffixSource func() string

// ShortNamePrefix cleans dir down to the directory part of a short name: the
// base name, lower cased, everything outside [a-z0-9-] dropped and the result cut
// to shortNameMaxDirLen characters (without leaving a "-" glued to the suffix).
// An empty result falls back to shortNameFallback.
func ShortNamePrefix(dir string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(filepath.Base(filepath.Clean(dir))) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > shortNameMaxDirLen {
		name = strings.Trim(name[:shortNameMaxDirLen], "-")
	}
	if name == "" {
		return shortNameFallback
	}
	return name
}

// NewShortName returns the short name of a chat process working in dir: the
// cleaned directory part plus a random suffix, retried while taken reports that
// the candidate is in use by another live chat process (nil taken means nothing
// is in use). After shortNameAttempts tries it returns a name with a longer
// suffix: a name is always needed, and two extra characters make a collision
// practically impossible.
func NewShortName(dir string, taken func(string) bool, suffix SuffixSource) string {
	prefix := ShortNamePrefix(dir)
	for attempt := 0; attempt < shortNameAttempts; attempt++ {
		name := prefix + "-" + suffix()
		if taken == nil || !taken(name) {
			return name
		}
	}
	return prefix + "-" + suffix() + suffix()
}

// RandomSuffix returns shortNameSuffixLen random characters of the short name
// alphabet: 1296 possible suffixes.
func RandomSuffix() string {
	var raw [shortNameSuffixLen]byte
	if _, err := crand.Read(raw[:]); err != nil {
		// crypto/rand does not fail on any platform suga supports; the clock keeps
		// the fallback from returning the same name twice in a row.
		seed := time.Now().UnixNano()
		for i := range raw {
			raw[i] = byte(seed >> (8 * i))
		}
	}
	out := make([]byte, len(raw))
	for i, v := range raw {
		// The modulo is slightly biased (256 is not a multiple of 36), which is
		// immaterial for picking one of 1296 names.
		out[i] = shortNameAlphabet[int(v)%len(shortNameAlphabet)]
	}
	return string(out)
}

// TakenNames returns a predicate reporting whether a short name is already used
// by another live chat process. ownPID's own record (the process that is about
// to register) is ignored, and so are records without a name (sessions that
// started before short names existed). Names compare without case, the same way
// Resolve matches them.
func TakenNames(records []Record, ownPID int) func(string) bool {
	taken := make(map[string]struct{}, len(records))
	for _, record := range records {
		name := strings.TrimSpace(record.Name)
		if record.PID == ownPID || name == "" {
			continue
		}
		taken[strings.ToLower(name)] = struct{}{}
	}
	return func(name string) bool {
		_, inUse := taken[strings.ToLower(strings.TrimSpace(name))]
		return inUse
	}
}
