package process

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxQueue = 8

type Record struct {
	PID       int       `json:"pid"`
	SessionID string    `json:"session_id"`
	// Name is the session's short name ("<workspace directory>-k7"), the address
	// `suga send --session <name>` accepts; empty in records of older versions,
	// which stored the first message as the name.
	Name      string    `json:"name"`
	CWD       string    `json:"cwd"`
	Kind      string    `json:"kind"`
	Socket    string    `json:"socket"`
	StartedAt time.Time `json:"started_at"`
	// ProcStart is the OS start time of PID; it tells a reused PID apart from this process.
	ProcStart string `json:"proc_start,omitempty"`
	Host      string `json:"host"`
	State     string `json:"state"`
}

type Request struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text,omitempty"`
	From      string `json:"from,omitempty"`
	// Wait asks the server to keep the connection open after a "queued" acknowledgement and send
	// a second Response once the message starts a turn (OK) or is discarded at exit (Error).
	Wait bool `json:"wait,omitempty"`
}

type Response struct {
	OK     bool   `json:"ok"`
	Queued bool   `json:"queued,omitempty"`
	Error  string `json:"error,omitempty"`
}

type DeliverFunc func(text, from string) (queued bool, err error)

// DeliverWaitFunc is a DeliverFunc that can also report the final fate of a queued message:
// settled (may be nil) yields nil once the message starts a turn, or an error if it is discarded.
type DeliverWaitFunc func(text, from string) (queued bool, settled <-chan error, err error)

// Error codes of the final Response sent to a waiting sender whose queued message was not run.
const (
	codeDiscarded     = "discarded"
	codeSessionClosed = "session_closed"
)

// ErrSessionClosed is returned by a deliver func that no longer accepts messages (chat is exiting).
var ErrSessionClosed = errors.New("session is closing")

// ErrDiscarded is the error a deliver func reports on a settled channel for a queued message dropped at exit.
var ErrDiscarded = errors.New("queued message discarded at exit")

type Server struct {
	root     string
	mu       sync.RWMutex
	record   Record
	listener net.Listener
	done     chan struct{}
	closing  chan struct{} // closed by Close so connections waiting on a queued message end promptly
	closeOne sync.Once
}

func Start(root, sessionID, name, cwd string, deliver DeliverFunc) (*Server, error) {
	return StartWait(root, sessionID, name, cwd, func(text, from string) (bool, <-chan error, error) {
		queued, err := deliver(text, from)
		return queued, nil, err
	})
}

// SecureDir restricts an existing directory to the current user: 0700 on
// Unix, a protected inheritable DACL (current user + SYSTEM) on Windows, so
// files created inside later get the same restriction.
func SecureDir(dir string) error { return secureDir(dir) }

func StartWait(root, sessionID, name, cwd string, deliver DeliverWaitFunc) (*Server, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("process registration requires root and session ID")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	// Restrict the directory to the current user (0700 on Unix, a protected DACL on Windows).
	if err := secureDir(root); err != nil {
		return nil, fmt.Errorf("secure %s: %w", root, err)
	}
	pid := os.Getpid()
	socket := filepath.Join(root, fmt.Sprintf("%d.sock", pid))
	if err := validateSocketPath(socket); err != nil {
		return nil, err
	}
	procStart := processStartID(pid)
	recordPath := filepath.Join(root, fmt.Sprintf("%d.json", pid))
	if data, err := os.ReadFile(recordPath); err == nil {
		var existing Record
		// Same PID and same start time is this very process; a different start time
		// is a crashed earlier owner of a now-reused PID.
		if json.Unmarshal(data, &existing) == nil && existing.PID == pid && existing.Host != "" && (procStart == "" || existing.ProcStart == procStart) {
			return nil, fmt.Errorf("this process already owns a chat registration")
		}
		_ = os.Remove(recordPath)
	}
	// Our own PID's socket can only be left behind by a dead owner (or one that died
	// between listening and registering), so it is always safe to remove here.
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on local session socket: %w", err)
	}
	_ = os.Chmod(socket, 0o600)
	host, _ := os.Hostname()
	record := Record{PID: pid, SessionID: sessionID, Name: name, CWD: cwd, Kind: "chat", Socket: socket, StartedAt: time.Now().UTC(), ProcStart: procStart, Host: host, State: "idle"}
	server := &Server{root: root, record: record, listener: listener, done: make(chan struct{}), closing: make(chan struct{})}
	if err := server.writeRecord(); err != nil {
		_ = listener.Close()
		_ = os.Remove(socket)
		return nil, err
	}
	go server.serve(deliver)
	return server, nil
}

func (s *Server) Record() Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.record
}

func (s *Server) SetBusy(busy bool) error {
	s.mu.Lock()
	if busy {
		s.record.State = "busy"
	} else {
		s.record.State = "idle"
	}
	s.mu.Unlock()
	return s.persistRecord()
}

func (s *Server) SetName(name string) error {
	s.mu.Lock()
	s.record.Name = name
	s.mu.Unlock()
	return s.persistRecord()
}

func (s *Server) persistRecord() error {
	copy := s.Record()
	data, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.root, fmt.Sprintf("%d.json.tmp", copy.PID))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.root, fmt.Sprintf("%d.json", copy.PID)))
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.closeOne.Do(func() { close(s.closing) })
	err := s.listener.Close()
	<-s.done
	_ = os.Remove(filepath.Join(s.root, fmt.Sprintf("%d.json", s.record.PID)))
	_ = os.Remove(s.record.Socket)
	return err
}

func (s *Server) writeRecord() error {
	data, err := json.Marshal(s.record)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.root, fmt.Sprintf("%d.json.tmp", s.record.PID))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.root, fmt.Sprintf("%d.json", s.record.PID)))
}

func (s *Server) serve(deliver DeliverWaitFunc) {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn, deliver)
	}
}

func (s *Server) handle(conn net.Conn, deliver DeliverWaitFunc) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var request Request
	if err := json.NewDecoder(bufio.NewReader(io.LimitReader(conn, 1<<20))).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "invalid_request"})
		return
	}
	if request.Type == "ping" {
		_ = json.NewEncoder(conn).Encode(Response{OK: true})
		return
	}
	if request.Type != "deliver" || request.SessionID != s.Record().SessionID || strings.TrimSpace(request.Text) == "" {
		_ = json.NewEncoder(conn).Encode(Response{Error: "invalid_request"})
		return
	}
	if deliver == nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "delivery_unavailable"})
		return
	}
	queued, settled, err := deliver(request.Text, request.From)
	if err != nil {
		code := "delivery_failed"
		if errors.Is(err, ErrQueueFull) {
			code = "queue_full"
		} else if errors.Is(err, ErrSessionClosed) {
			code = codeSessionClosed
		}
		_ = json.NewEncoder(conn).Encode(Response{Error: code})
		return
	}
	encoder := json.NewEncoder(conn)
	_ = encoder.Encode(Response{OK: true, Queued: queued})
	if !request.Wait || !queued || settled == nil {
		return
	}
	// Two-phase reply: the sender already has the "queued" ack; keep the connection open (no
	// deadline, the wait is as long as the current turn) and report whether the message ran.
	_ = conn.SetDeadline(time.Time{})
	select {
	case err := <-settled:
		if err == nil {
			_ = encoder.Encode(Response{OK: true, Queued: true})
		} else {
			_ = encoder.Encode(Response{Queued: true, Error: codeDiscarded})
		}
	case <-s.closing:
		_ = encoder.Encode(Response{Queued: true, Error: codeSessionClosed})
	}
}

var ErrQueueFull = errors.New("delivery queue is full")

func List(root string) ([]Record, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record Record
		if json.Unmarshal(data, &record) != nil || record.PID <= 0 || filepath.Base(entry.Name()) != fmt.Sprintf("%d.json", record.PID) || filepath.Clean(record.Socket) != filepath.Clean(filepath.Join(root, fmt.Sprintf("%d.sock", record.PID))) {
			continue
		}
		if !processAlive(record.PID, record.ProcStart) {
			_ = os.Remove(path)
			_ = os.Remove(filepath.Join(root, fmt.Sprintf("%d.sock", record.PID)))
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SessionID < records[j].SessionID })
	return records, nil
}

func Send(record Record, request Request) (Response, error) {
	if !processAlive(record.PID, record.ProcStart) {
		return Response{}, ErrNotOnline
	}
	conn, err := net.DialTimeout("unix", record.Socket, 2*time.Second)
	if err != nil {
		// The owner may have exited between the liveness check and the dial.
		if !processAlive(record.PID, record.ProcStart) {
			return Response{}, ErrNotOnline
		}
		return Response{}, fmt.Errorf("connect session socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if request.Type == "" {
		request.Type = "deliver"
	}
	request.SessionID = record.SessionID
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, err
	}
	decoder := json.NewDecoder(conn)
	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error == "queue_full" {
		return response, ErrQueueFull
	}
	if response.Error == codeSessionClosed {
		return response, ErrNotOnline
	}
	if !response.OK {
		return response, fmt.Errorf("delivery failed: %s", response.Error)
	}
	if !request.Wait || !response.Queued {
		return response, nil
	}
	// Wait for the final fate of the queued message. A dropped connection means the chat died.
	_ = conn.SetDeadline(time.Time{})
	var final Response
	if err := decoder.Decode(&final); err != nil {
		return response, fmt.Errorf("%w: connection closed before the queued message ran", ErrNotOnline)
	}
	if final.Error == codeDiscarded || final.Error == codeSessionClosed {
		return final, fmt.Errorf("%w: queued message was discarded when the chat exited", ErrNotOnline)
	}
	if !final.OK {
		return final, fmt.Errorf("delivery failed: %s", final.Error)
	}
	return final, nil
}

var ErrNotOnline = errors.New("session process is not online")

// processAlive reports whether pid still runs the process that recorded procStart.
// An empty procStart (older record or unsupported platform) falls back to the PID alone.
func processAlive(pid int, procStart string) bool {
	if !processExists(pid) {
		return false
	}
	if procStart == "" {
		return true
	}
	current := processStartID(pid)
	return current == "" || current == procStart
}

// Resolve picks the live process a selector names. Three stages, each only
// ambiguous within itself: an exact session ID, then an exact short name
// (ignoring case), then a session ID prefix. The order matters, a full ID that
// is also another session's prefix or short name never turns ambiguous, and a
// short name that is also a prefix of several IDs still addresses one process.
func Resolve(records []Record, selector string) (Record, error) {
	selector = strings.TrimSpace(selector)
	// An empty selector would match every record without a short name below.
	if selector == "" {
		return Record{}, ErrNotOnline
	}
	stages := []func(Record) bool{
		func(record Record) bool { return record.SessionID == selector },
		func(record Record) bool { return strings.EqualFold(record.Name, selector) },
		func(record Record) bool { return strings.HasPrefix(record.SessionID, selector) },
	}
	for _, matches := range stages {
		var picked []Record
		for _, record := range records {
			if matches(record) {
				picked = append(picked, record)
			}
		}
		if len(picked) > 1 {
			return Record{}, ErrAmbiguous
		}
		if len(picked) == 1 {
			return picked[0], nil
		}
	}
	return Record{}, ErrNotOnline
}

var ErrAmbiguous = errors.New("session selector matches multiple processes")
