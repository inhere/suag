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

// ChatHighlightInputEnabled reports whether chat paints a background behind the
// line the person submitted at the prompt, and behind messages delivered from
// elsewhere (chat.highlight_input, on by default). It takes the same values as
// chat.markdown; a non-terminal output or NO_COLOR turns the background off.
func ChatHighlightInputEnabled() bool {
	return chatSwitch("chat.highlight_input")
}

// ChatTerminalTitleEnabled reports whether chat sets the terminal's window/tab
// title to the session, "suga · <短名称> · <会话标题>" (chat.terminal_title, on by
// default). It takes the same values as chat.markdown; a non-terminal output or
// NO_COLOR leaves the title alone anyway.
func ChatTerminalTitleEnabled() bool {
	return chatSwitch("chat.terminal_title")
}

// ChatAskOutsideDirs reports whether an interactive chat asks the person to
// authorize a directory a file tool wants to reach outside the workspace and the
// session's added directories (chat.ask_outside_dirs, on by default). It takes
// the same values as chat.markdown. The question is asked on the terminal, so
// run, ACP and a chat without a terminal keep refusing such a call as before.
func ChatAskOutsideDirs() bool {
	return chatSwitch("chat.ask_outside_dirs")
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
