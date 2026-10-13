package axconfig

import (
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// ChatMarkdownEnabled reports whether chat renders the model's markdown in the
// terminal (chat.markdown, on by default). The key accepts a bool or a string;
// "off"/"false" turn the rendering off and show the exact model text instead.
func ChatMarkdownEnabled() bool {
	return chatSwitch("chat.markdown")
}

// ChatSpinnerEnabled reports whether chat shows its "思考中…" waiting line while
// the model is asked for its next message (chat.spinner, on by default). It
// takes the same values as chat.markdown; a non-terminal output or NO_COLOR
// turns the line off anyway.
func ChatSpinnerEnabled() bool {
	return chatSwitch("chat.spinner")
}

// chatSwitch reads a chat.* switch: a missing key is on, a bool is used as is,
// and a string is off for "off"/"false" (any case, surrounding spaces ignored).
func chatSwitch(key string) bool {
	switch value := gkconfig.Get(key).(type) {
	case bool:
		return value
	case string:
		return !strings.EqualFold(strings.TrimSpace(value), "off") && !strings.EqualFold(strings.TrimSpace(value), "false")
	default:
		return true
	}
}
