// Package tasks runs work in the background for an agent session: start a
// command (or a function: a watch loop, a hook, later a sub-agent) and get an
// id at once, read its output while it runs, wait for it with a timeout, kill
// it. Output goes line by line (cleaned by the caller, e.g. decoded and
// redacted) to a file the host names per session. A finished task leaves a
// notice the host hands to the model before its next request (TakeNotices),
// unless the model already saw the end through Wait or Kill; a running
// function task may leave notices too (Reporter.Notify). The whole process
// tree of every command task is killed by Kill, by the limits and by Close;
// a function task's context is cancelled. Nothing outlives the manager.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/inhere/suag/pkg/agentcore/internal/proctree"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

// Status is where a task is in its life.
type Status string

const (
	Running  Status = "running"
	Exited   Status = "exited"    // the command ended by itself (any exit code)
	Killed   Status = "killed"    // Kill or Close ended it
	TimedOut Status = "timed_out" // it ran past Limits.MaxDuration
	Failed   Status = "failed"    // it could not be waited for (rare)
)

// Limits bound the tasks of a manager.
type Limits struct {
	// MaxRunning is how many tasks of one session may run at once.
	MaxRunning int
	// MaxDuration ends a task that runs longer.
	MaxDuration time.Duration
	// MaxOutputBytes stops writing a task's output file past this size.
	MaxOutputBytes int64
}

// DefaultLimits: 4 running per session, 2 hours each, 64 MiB of output.
var DefaultLimits = Limits{MaxRunning: 4, MaxDuration: 2 * time.Hour, MaxOutputBytes: 64 << 20}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits
	if l.MaxRunning > 0 {
		d.MaxRunning = l.MaxRunning
	}
	if l.MaxDuration > 0 {
		d.MaxDuration = l.MaxDuration
	}
	if l.MaxOutputBytes > 0 {
		d.MaxOutputBytes = l.MaxOutputBytes
	}
	return d
}

// Config sets up a Manager.
type Config struct {
	Limits Limits
	// Output names the output file of a task and its ref (a handle the
	// model can pass to other tools, e.g. "out:<session>:<hex>"). Required.
	Output func(sessionID, taskID string) (path, ref string, err error)
	// Session returns the session a call belongs to; nil uses
	// llm.SessionIDFrom.
	Session func(ctx context.Context) string
	// OnNotice, when set, is called for every notice left pending: a task
	// that ended unseen, or a running task's Reporter.Notify (e.g. to wake an
	// idle chat). It runs in the task's goroutine and must not block.
	OnNotice func(Info)
}

// Reporter is what a function task (Spec.Run) tells the manager while it runs.
type Reporter interface {
	// Notify leaves a notice for the model now, while the task keeps running
	// (e.g. a watch whose result changed). A newer notice of the same task
	// replaces one the host has not taken yet.
	Notify(text string)
	// Progress sets a short state shown with the task (e.g. "run 5, exit 0").
	Progress(text string)
	// Count sets a named counter reported with the task (Info.Counters).
	Count(name string, n int)
}

// RunFunc is a function task: it writes its output to out (line by line,
// cleaned like a command's) and returns its exit code. ctx is cancelled by
// Kill, by Limits.MaxDuration and by Close; a non-nil error marks it failed.
type RunFunc func(ctx context.Context, out io.Writer, r Reporter) (exitCode int, err error)

// Spec is one task to start: a command (Cmd) or a function (Run).
type Spec struct {
	Kind    string // "shell", "watch", "hook"
	Summary string // what it runs, shown in lists and notices
	// Cmd is ready to Start (path, args, dir, env); its Stdout, Stderr and
	// SysProcAttr are set by the manager.
	Cmd *exec.Cmd
	// Run is a function task, instead of Cmd.
	Run RunFunc
	// NoEndNotice: the task reports what matters through Reporter.Notify,
	// so its end leaves no notice (a Stop hook run).
	NoEndNotice bool
	// Clean turns raw output into text for the file and the tail, one line
	// at a time (no trailing newline); nil keeps the bytes as they are.
	Clean func(line []byte) string
}

// Info is a snapshot of a task.
type Info struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	Status    Status    `json:"status"`
	ExitCode  int       `json:"exit_code"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitzero"`
	// Bytes and Lines count what was written to the output file.
	Bytes     int64  `json:"bytes"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated,omitempty"` // output past MaxOutputBytes was dropped
	Path      string `json:"path"`
	Ref       string `json:"ref"`
	// Tail is the last lines of the output (up to tailLines / tailBytes).
	Tail []string `json:"tail,omitempty"`
	// Error is why a function task failed.
	Error string `json:"error,omitempty"`
	// Progress and Counters are what a function task reported (Reporter).
	Progress string         `json:"progress,omitempty"`
	Counters map[string]int `json:"counters,omitempty"`
	// Notice is set on a notice of a running task (TakeNotices): its text.
	// Empty on an end notice.
	Notice string `json:"notice,omitempty"`
}

// Duration is how long the task ran (so far, while running).
func (i Info) Duration() time.Duration {
	if i.Ended.IsZero() {
		return time.Since(i.Started)
	}
	return i.Ended.Sub(i.Started)
}

// ErrUnknownTask is returned for an id the session does not have.
var ErrUnknownTask = errors.New("unknown task")

// Manager owns the background tasks of one host process.
type Manager struct {
	cfg    Config
	limits Limits

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
	// onEnd is told of every task end (SetOnEnd).
	onEnd atomic.Pointer[func(Info)]
}

// SetOnEnd sets a function told (in the task's goroutine) of every task that
// ends, seen or not, e.g. to record it in the trace; the latest call wins.
func (m *Manager) SetOnEnd(fn func(Info)) { m.onEnd.Store(&fn) }

type session struct {
	seq     int
	tasks   map[string]*task
	order   []string
	notices []pending // notices not taken yet, in order
}

// pending is one notice: a task's end (text "") or a running task's Notify.
type pending struct {
	id   string
	text string
}

// NewManager returns a Manager; cfg.Output is required.
func NewManager(cfg Config) *Manager {
	if cfg.Session == nil {
		cfg.Session = llm.SessionIDFrom
	}
	return &Manager{cfg: cfg, limits: cfg.Limits.withDefaults(), sessions: map[string]*session{}}
}

// Limits returns the effective limits.
func (m *Manager) Limits() Limits { return m.limits }

// SessionOf returns the session of ctx (Config.Session).
func (m *Manager) SessionOf(ctx context.Context) string { return m.cfg.Session(ctx) }

func (m *Manager) sessionLocked(id string) *session {
	s := m.sessions[id]
	if s == nil {
		s = &session{tasks: map[string]*task{}}
		m.sessions[id] = s
	}
	return s
}

// Start runs spec in the background for the session of ctx and returns its
// snapshot at once.
func (m *Manager) Start(ctx context.Context, spec Spec) (Info, error) {
	sid := m.cfg.Session(ctx)
	if sid == "" {
		return Info{}, errors.New("background tasks need a session")
	}
	if (spec.Cmd == nil) == (spec.Run == nil) {
		return Info{}, errors.New("a task needs a command or a function")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Info{}, errors.New("background tasks are shut down")
	}
	s := m.sessionLocked(sid)
	running := 0
	for _, t := range s.tasks {
		if t.status() == Running {
			running++
		}
	}
	if running >= m.limits.MaxRunning {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("%d background tasks are already running (the limit): wait for or kill one first", running)
	}
	s.seq++
	id := "t" + strconv.Itoa(s.seq)
	m.mu.Unlock()

	path, ref, err := m.cfg.Output(sid, id)
	if err != nil {
		return Info{}, fmt.Errorf("task output: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("task output: %w", err)
	}
	t := &task{
		info: Info{ID: id, SessionID: sid, Kind: spec.Kind, Summary: spec.Summary, Status: Running,
			ExitCode: -1, Started: time.Now(), Path: path, Ref: ref},
		done: make(chan struct{}),
		seen: spec.NoEndNotice,
	}
	t.out = newLineWriter(file, spec.Clean, m.limits.MaxOutputBytes)
	if spec.Run != nil {
		ctx, cancel := context.WithCancel(context.Background())
		t.cancel = cancel
		m.register(s, t)
		go m.runFunc(s, t, ctx, spec.Run)
		return t.snapshot(), nil
	}
	cmd := spec.Cmd
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = t.out, t.out
	// A leftover child holding the pipe must not keep the task "running"
	// after the command itself ended.
	cmd.WaitDelay = 2 * time.Second
	t.tree = proctree.New(cmd)
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		return Info{}, fmt.Errorf("start: %w", err)
	}
	t.tree.Started()
	m.register(s, t)
	go m.wait(s, t, cmd)
	return t.snapshot(), nil
}

// register adds a started task to its session and arms its time limit.
func (m *Manager) register(s *session, t *task) {
	m.mu.Lock()
	s.tasks[t.info.ID] = t
	s.order = append(s.order, t.info.ID)
	m.mu.Unlock()
	t.timer = time.AfterFunc(m.limits.MaxDuration, func() { t.end(TimedOut) })
}

// wait reaps the command and records how it ended.
func (m *Manager) wait(s *session, t *task, cmd *exec.Cmd) {
	err := cmd.Wait()
	t.tree.Close() // also ends children left running in the background
	code := cmd.ProcessState.ExitCode()
	status := Exited
	if err != nil && code < 0 {
		status = Failed
	}
	m.finish(s, t, status, code, "")
}

// runFunc runs a function task and records how it ended.
func (m *Manager) runFunc(s *session, t *task, ctx context.Context, run RunFunc) {
	code, err := run(ctx, t.out, reporter{m: m, s: s, t: t})
	t.cancel()
	status, msg := Exited, ""
	if err != nil {
		status, msg = Failed, err.Error()
	}
	m.finish(s, t, status, code, msg)
}

// finish records a task's end (status unless Kill or the time limit ended
// it), tells the host and leaves the end notice unless the model saw it.
func (m *Manager) finish(s *session, t *task, status Status, code int, errMsg string) {
	t.timer.Stop()
	t.out.finish()
	t.mu.Lock()
	t.info.Ended = time.Now()
	t.info.ExitCode = code
	t.info.Status = status
	if t.ending != "" {
		t.info.Status = t.ending
	} else if status == Failed {
		t.info.Error = errMsg
	}
	t.mu.Unlock()
	// The end is recorded before Wait and Kill return: a host that exits
	// right after Kill (run) still gets it into its store.
	if fn := m.onEnd.Load(); fn != nil && *fn != nil {
		(*fn)(t.snapshot())
	}
	t.mu.Lock()
	seen := t.seen
	t.mu.Unlock()
	close(t.done)
	m.mu.Lock()
	if !seen {
		s.notices = append(s.notices, pending{id: t.info.ID})
	}
	m.mu.Unlock()
	if !seen && m.cfg.OnNotice != nil {
		m.cfg.OnNotice(t.snapshot())
	}
}

// reporter is the Reporter of one function task.
type reporter struct {
	m *Manager
	s *session
	t *task
}

func (r reporter) Notify(text string) {
	if text == "" {
		return
	}
	r.m.mu.Lock()
	// A newer notice replaces the task's pending one (not its end notice).
	r.s.notices = slices.DeleteFunc(r.s.notices, func(p pending) bool { return p.id == r.t.info.ID && p.text != "" })
	r.s.notices = append(r.s.notices, pending{id: r.t.info.ID, text: text})
	r.m.mu.Unlock()
	if r.m.cfg.OnNotice != nil {
		info := r.t.snapshot()
		info.Notice = text
		r.m.cfg.OnNotice(info)
	}
}

func (r reporter) Progress(text string) {
	r.t.mu.Lock()
	r.t.info.Progress = text
	r.t.mu.Unlock()
}

func (r reporter) Count(name string, n int) {
	r.t.mu.Lock()
	if r.t.info.Counters == nil {
		r.t.info.Counters = map[string]int{}
	}
	r.t.info.Counters[name] = n
	r.t.mu.Unlock()
}

// List returns the session's tasks, in start order.
func (m *Manager) List(sessionID string) []Info {
	m.mu.Lock()
	s := m.sessions[sessionID]
	var tasks []*task
	if s != nil {
		for _, id := range s.order {
			tasks = append(tasks, s.tasks[id])
		}
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.snapshot())
	}
	return out
}

func (m *Manager) get(sessionID, id string) (*task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[sessionID]; s != nil {
		if t := s.tasks[id]; t != nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownTask, id)
}

// Get returns one task's snapshot.
func (m *Manager) Get(sessionID, id string) (Info, error) {
	t, err := m.get(sessionID, id)
	if err != nil {
		return Info{}, err
	}
	return t.snapshot(), nil
}

// Wait waits up to timeout for the task to end and reports whether it did.
// An ended task's notice is dropped: the caller saw the end.
func (m *Manager) Wait(ctx context.Context, sessionID, id string, timeout time.Duration) (Info, bool, error) {
	t, err := m.get(sessionID, id)
	if err != nil {
		return Info{}, false, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-t.done:
		m.markSeen(sessionID, t)
		return t.snapshot(), true, nil
	case <-timer.C:
		return t.snapshot(), false, nil
	case <-ctx.Done():
		return t.snapshot(), false, ctx.Err()
	}
}

// Kill ends the task's process tree and waits (briefly) for it to be reaped.
func (m *Manager) Kill(sessionID, id string) (Info, error) {
	t, err := m.get(sessionID, id)
	if err != nil {
		return Info{}, err
	}
	t.end(Killed)
	select {
	case <-t.done:
	case <-time.After(5 * time.Second):
	}
	m.markSeen(sessionID, t)
	return t.snapshot(), nil
}

// markSeen drops the pending notice of an ended task.
func (m *Manager) markSeen(sessionID string, t *task) {
	t.mu.Lock()
	t.seen = true
	t.mu.Unlock()
	m.mu.Lock()
	if s := m.sessions[sessionID]; s != nil {
		s.notices = slices.DeleteFunc(s.notices, func(p pending) bool { return p.id == t.info.ID && p.text == "" })
	}
	m.mu.Unlock()
}

// TakeNotices returns the session's ended tasks whose notice is pending and
// clears them.
func (m *Manager) TakeNotices(sessionID string) []Info {
	m.mu.Lock()
	s := m.sessions[sessionID]
	var taken []pending
	if s != nil {
		taken, s.notices = s.notices, nil
	}
	tasks := make([]*task, len(taken))
	for i, p := range taken {
		tasks[i] = s.tasks[p.id]
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(taken))
	for i, t := range tasks {
		if taken[i].text == "" { // an end notice: the model sees the end
			t.mu.Lock()
			t.seen = true
			t.mu.Unlock()
		}
		info := t.snapshot()
		info.Notice = taken[i].text
		out = append(out, info)
	}
	return out
}

// HasNotices reports whether the session has pending notices.
func (m *Manager) HasNotices(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[sessionID]
	return s != nil && len(s.notices) > 0
}

// Running returns the session's running tasks.
func (m *Manager) Running(sessionID string) []Info {
	var out []Info
	for _, i := range m.List(sessionID) {
		if i.Status == Running {
			out = append(out, i)
		}
	}
	return out
}

// KillSession kills the session's running tasks and returns them.
func (m *Manager) KillSession(sessionID string) []Info {
	var out []Info
	for _, i := range m.Running(sessionID) {
		if info, err := m.Kill(sessionID, i.ID); err == nil {
			out = append(out, info)
		}
	}
	return out
}

// Close kills every running task and refuses new ones.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	var ids []string
	for sid := range m.sessions {
		ids = append(ids, sid)
	}
	m.mu.Unlock()
	for _, sid := range ids {
		m.KillSession(sid)
	}
}

// task is one background command or function.
type task struct {
	mu     sync.Mutex
	info   Info
	ending Status             // set by end before the kill: how the end is reported
	seen   bool               // the model saw the end (Wait, Kill or a notice)
	tree   *proctree.Tree     // a command's process tree
	cancel context.CancelFunc // a function's context
	out    *lineWriter
	timer  *time.Timer
	done   chan struct{}
}

func (t *task) status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.info.Status
}

// end kills the task, reporting it as how (unless it already ended).
func (t *task) end(how Status) {
	t.mu.Lock()
	if t.info.Status != Running || t.ending != "" {
		t.mu.Unlock()
		return
	}
	t.ending = how
	t.mu.Unlock()
	if t.tree != nil {
		_ = t.tree.Kill()
	} else {
		t.cancel()
	}
}

func (t *task) snapshot() Info {
	t.mu.Lock()
	info := t.info
	info.Counters = maps.Clone(t.info.Counters)
	t.mu.Unlock()
	info.Bytes, info.Lines, info.Truncated, info.Tail = t.out.stats()
	return info
}
