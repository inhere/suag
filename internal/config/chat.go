package axconfig

import (
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// ChatMarkdownEnabled reports whether chat renders the model's markdown in the
// terminal (chat.markdown, on by default). The key accepts a bool or a string;
// "off"/"false" turn the rendering off and show the exact model text instead.
func ChatMarkdownEnabled() bool {
	switch value := gkconfig.Get("chat.markdown").(type) {
	case bool:
		return value
	case string:
		return !strings.EqualFold(strings.TrimSpace(value), "off") && !strings.EqualFold(strings.TrimSpace(value), "false")
	default:
		return true
	}
}
