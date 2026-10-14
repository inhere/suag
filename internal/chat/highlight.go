package chat

// Highlighting of input lines (chat.highlight_input, on by default): the line
// the person submitted at the prompt, and the messages delivered from elsewhere
// (Renderer.External), are repainted on a background of their own so they stand
// apart from the model's answer.
//
// Why these colors: the background and the foreground are both set, because the
// terminal's own foreground is unknown - the person may run a dark or a light
// theme, and a gray background alone would come out unreadable in one of them
// (dark text on a dark gray, or the other way round). The pairs are low
// saturation and stay readable either way:
//   - the person's own line: 256-color 236 (#303030, nearly black gray) with 253
//     (#dadada) text;
//   - a message from elsewhere, which chat paints magenta without highlighting:
//     the same idea in a muted purple, 54 (#5f005f) with 225 (#ffd7ff) text, so
//     the two are told apart at a glance.
//
// 256-color (and not true color) values: they are the widest common ground, and
// every terminal chat already writes colors to understands them (NO_COLOR,
// a pipe or a redirected output never gets here at all).
const (
	inputHighlightPrefix    = "\x1b[48;5;236m\x1b[38;5;253m"
	externalHighlightPrefix = "\x1b[48;5;54m\x1b[38;5;225m"
	// highlightSuffix closes either style: the background, the foreground and
	// everything the prompt itself painted before it.
	highlightSuffix = "\x1b[0m"
)
