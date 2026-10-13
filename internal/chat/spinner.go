package chat

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gookit/cliui/progress"
)

const (
	// spinnerSpeed is the interval between two frames of the waiting line.
	// It is cliui's default spinner speed: fast enough to look alive, slow
	// enough not to flood the terminal.
	spinnerSpeed = 100 * time.Millisecond
	// spinnerClear moves the cursor to the start of the line and erases it:
	// the overwrite sequence cliui's progress widgets use for a dynamic line.
	// Every frame starts with it (the elapsed seconds change anyway) and Stop
	// writes it once more, so the output that follows starts at column 0 with
	// no trace of the hint left on screen.
	spinnerClear = "\r\x1b[2K"
	// spinnerLabel leads the waiting line; the elapsed seconds follow it.
	spinnerLabel = "思考中…"
)

// spinnerChars is cliui's dots loading theme (progress.CharsThemes[20]): the
// animation of the waiting line.
var spinnerChars = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// thinkingSpinner shows one self-updating line while chat waits for the model
// ("⠋ 思考中… 3s", dimmed on a colored terminal). The model is asked for its
// next message at every turn start and after every tool result; the line ends
// with the model's first output or the first line chat prints itself.
//
// It does not use cliui's progress.SpinnerFactory (Spinner/LoadingSpinner): that
// widget renders from a goroutine it owns with an internal time.Sleep, so a test
// cannot drive its clock, Stop returns without waiting for that goroutine (a
// frame could land between the erase and the renderer's next line) and nothing
// reports that the goroutine is gone. This type keeps cliui's animation
// (progress.LoadingWidget) and its overwrite convention but owns the frame loop.
type thinkingSpinner struct {
	enabled bool
	out     io.Writer
	// paint styles a frame (a colored terminal); nil keeps it plain.
	paint func(string) string
	// speed is the frame interval; now and ticks are injectable, so a test can
	// drive the line without sleeping (nil: the real clock and a time.Ticker).
	speed time.Duration
	now   func() time.Time
	ticks func(time.Duration) (<-chan time.Time, func())
	// frame draws the animation character of the next frame: cliui's loading
	// widget, which ignores its progress argument.
	frame progress.WidgetFunc

	mu      sync.Mutex // guards the state below, the frames and Stop's erase
	active  bool       // a frame goroutine is running
	drew    bool       // a frame was written: Stop must erase the line
	started time.Time  // the line appeared at; the elapsed seconds count from it
	stopCh  chan struct{}
	done    chan struct{}
	cancel  func() // releases the ticker of the running frame goroutine
}

// spinnerConfig configures a thinkingSpinner.
type spinnerConfig struct {
	enabled bool
	out     io.Writer
	paint   func(string) string
	speed   time.Duration
	now     func() time.Time
	ticks   func(time.Duration) (<-chan time.Time, func())
}

func newThinkingSpinner(cfg spinnerConfig) *thinkingSpinner {
	s := &thinkingSpinner{
		enabled: cfg.enabled, out: cfg.out, paint: cfg.paint, speed: cfg.speed,
		now: cfg.now, ticks: cfg.ticks, frame: progress.LoadingWidget(spinnerChars),
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.ticks == nil {
		s.ticks = realTicks
	}
	if s.speed <= 0 {
		s.speed = spinnerSpeed
	}
	return s
}

// realTicks is the default frame cadence: a real ticker, released by the
// returned func.
func realTicks(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

// Start shows the waiting line. It does nothing while the spinner is off (not a
// terminal, NO_COLOR or chat.spinner: false) or the line is already shown, so
// it may be called again after every tool result.
func (s *thinkingSpinner) Start() {
	if !s.enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return
	}
	s.active, s.drew, s.started = true, false, s.now()
	s.stopCh, s.done = make(chan struct{}), make(chan struct{})
	tick, cancel := s.ticks(s.speed)
	s.cancel = cancel
	go s.run(tick, s.stopCh, s.done)
}

// Stop erases the waiting line and waits for the frame goroutine to be gone, so
// its caller can print immediately afterwards knowing that nothing will overwrite
// it and no goroutine is left behind. It is safe to call when nothing is shown.
func (s *thinkingSpinner) Stop() {
	if !s.enabled {
		return
	}
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	stopCh, done, cancel, drew := s.stopCh, s.done, s.cancel, s.drew
	s.stopCh, s.done, s.cancel, s.drew = nil, nil, nil, false
	s.mu.Unlock()

	close(stopCh)
	if cancel != nil {
		cancel()
	}
	<-done // no frame can follow this erase

	if drew {
		// Holding mu keeps a frame of an immediately following Start from
		// slipping in between.
		s.mu.Lock()
		fmt.Fprint(s.out, spinnerClear)
		s.mu.Unlock()
	}
}

// run draws one frame per tick until Stop closes stopCh.
func (s *thinkingSpinner) run(tick <-chan time.Time, stopCh, done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-stopCh:
			return
		case <-tick:
			s.draw()
		}
	}
}

// draw rewrites the line. mu is held for the whole write, so a frame never
// slips between Stop's erase and the renderer's next output.
func (s *thinkingSpinner) draw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		// Stopped while this frame was in flight (its tick was already
		// delivered): the text must not appear after the erase.
		return
	}
	fmt.Fprint(s.out, spinnerClear+s.line())
	s.drew = true
}

// line is the text of one frame: the animation character, the label and the
// seconds waited so far.
func (s *thinkingSpinner) line() string {
	elapsed := int(s.now().Sub(s.started).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}
	text := fmt.Sprintf("%s %s %ds", s.frame(nil), spinnerLabel, elapsed)
	if s.paint != nil {
		text = s.paint(text)
	}
	return text
}
