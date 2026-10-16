package chat

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// notifyMode selects the terminal notification protocol.
type notifyMode string

const (
	notifyAuto notifyMode = "auto"
	notifyBell notifyMode = "bell"
	notifyOSC9 notifyMode = "osc9"
	notifyOff  notifyMode = "off"
)

// chatNotifier emits one short notification only when chat owns a real terminal.
type chatNotifier struct {
	mode  notifyMode
	after time.Duration
	out   io.Writer
	envWT bool
}

func (n chatNotifier) enabled() bool { return n.mode != notifyOff && n.out != nil }

// shouldNotify applies the configured duration threshold.
func (n chatNotifier) shouldNotify(elapsed time.Duration) bool {
	return n.enabled() && elapsed >= n.after
}

// notify writes BEL for ordinary terminals and OSC 9 for Windows Terminal.
func (n chatNotifier) notify(text string) {
	if !n.enabled() {
		return
	}
	mode := n.mode
	if mode == notifyAuto {
		if n.envWT {
			mode = notifyOSC9
		} else {
			mode = notifyBell
		}
	}
	switch mode {
	case notifyOSC9:
		fmt.Fprintf(n.out, "\x1b]9;%s\a", strings.ReplaceAll(text, "\a", ""))
	case notifyBell:
		_, _ = io.WriteString(n.out, "\a")
	}
}

func newChatNotifier(mode string, after time.Duration, out io.Writer, terminal bool) chatNotifier {
	if !terminal {
		return chatNotifier{mode: notifyOff}
	}
	if after <= 0 {
		after = time.Minute
	}
	return chatNotifier{mode: notifyMode(mode), after: after, out: out, envWT: os.Getenv("WT_SESSION") != ""}
}
