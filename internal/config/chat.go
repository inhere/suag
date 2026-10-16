package axconfig

import (
	"fmt"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// ChatHistoryEnabled reports whether locally typed chat lines are persisted.
func ChatHistoryEnabled() bool { return chatSwitch("chat.history") }

// ChatHistorySize returns the maximum number of persisted chat lines.
func ChatHistorySize() (int, error) {
	if gkconfig.Get("chat.history_size") == nil {
		return 1000, nil
	}
	raw := strings.TrimSpace(gkconfig.String("chat.history_size"))
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("chat.history_size must be a whole number >= 1, got %q", raw)
	}
	return n, nil
}

// ChatMarkdownEnabled reports whether chat renders the model's markdown in the
// terminal (chat.markdown, on by default).
func ChatMarkdownEnabled() bool       { return chatSwitch("chat.markdown") }
func ChatSpinnerEnabled() bool        { return chatSwitch("chat.spinner") }
func ChatHighlightInputEnabled() bool { return chatSwitch("chat.highlight_input") }
func ChatSteeringEnabled() bool       { return chatSwitch("chat.steering") }
func ChatTerminalTitleEnabled() bool  { return chatSwitch("chat.terminal_title") }
func ChatDiffEnabled() bool           { return chatSwitch("chat.diff") }

const DefaultChatDiffLines = 20

func ChatDiffLines() (int, error) {
	if gkconfig.Get("chat.diff_lines") == nil {
		return DefaultChatDiffLines, nil
	}
	raw := strings.TrimSpace(gkconfig.String("chat.diff_lines"))
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("chat.diff_lines must be a whole number >= 1, got %q", raw)
	}
	return n, nil
}
func ChatAskOutsideDirs() bool { return chatSwitch("chat.ask_outside_dirs") }
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
