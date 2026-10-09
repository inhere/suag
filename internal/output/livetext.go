package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

// LiveText prints a turn's text deltas as they stream in. Each model message
// ends on its own line, so commentary before a tool call and the final answer
// do not run together.
type LiveText struct {
	w        io.Writer
	streamed bool // any delta printed this turn
	open     bool // the last printed delta did not end with a newline
}

// NewLiveText returns a LiveText writing to w.
func NewLiveText(w io.Writer) *LiveText { return &LiveText{w: w} }

// Handle prints a text delta or closes the line on message_stop; it reports
// whether the event was a streaming event.
func (l *LiveText) Handle(event personal.Event) bool {
	switch event.Kind {
	case string(agentcore.StreamEventTextDelta):
		if event.Text != "" {
			fmt.Fprint(l.w, event.Text)
			l.streamed, l.open = true, !strings.HasSuffix(event.Text, "\n")
		}
		return true
	case string(agentcore.StreamEventMessageStop):
		l.closeLine()
		return true
	}
	return false
}

func (l *LiveText) closeLine() {
	if l.open {
		fmt.Fprintln(l.w)
		l.open = false
	}
}

// Finish ends the turn's output and reports whether anything was streamed.
func (l *LiveText) Finish() bool {
	l.closeLine()
	return l.streamed
}
