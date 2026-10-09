package axconfig

import (
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// TranscriptsEnabled reports whether chat transcripts are written. The
// `transcripts` key accepts a bool or a string; "off"/"false" disable it.
func TranscriptsEnabled() bool {
	value := gkconfig.Get("transcripts")
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return !strings.EqualFold(strings.TrimSpace(v), "off") && !strings.EqualFold(strings.TrimSpace(v), "false")
	default:
		return true
	}
}
