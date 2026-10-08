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
	Name      string    `json:"name"`
	CWD       string    `json:"cwd"`
	Kind      string    `json:"kind"`
	Socket    string    `json:"socket"`
	StartedAt time.Time `json:"started_at"`
	Host      string    `json:"host"`
	State     string    `json:"state"`
}

type Request struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text,omitempty"`
	From      string `json:"from,omitempty"`
}

type Response struct {
	OK     bool   `json:"ok"`
	Queued bool   `json:"queued,omitempty"`
	Error  string `json:"error,omitempty"`
}

type DeliverFunc func(text, from string) (queued bool, err error)

type Server struct {
	root     string
	mu       sync.RWMutex
	record   Record
	listener net.Listener
	done     chan struct{}
}

func Start(root, sessionID, name, cwd string, deliver DeliverFunc) (*Server, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("process registration requires root and session ID")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(root, 0o700)
	pid := os.Getpid()
	socket := filepath.Join(root, fmt.Sprintf("%d.sock", pid))
	if err := validateSocketPath(socket); err != nil {
		return nil, err
	}
	recordPath := filepath.Join(root, fmt.Sprintf("%d.json", pid))
	if data, err := os.ReadFile(recordPath); err == nil {
		var existing Record
		if json.Unmarshal(data, &existing) == nil && existing.PID == pid && existing.Host != "" {
			return nil, fmt.Errorf("this process already owns a chat registration")
		}
		_ = os.Remove(recordPath)
		_ = os.Remove(socket)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on local session socket: %w", err)
	}
	_ = os.Chmod(socket, 0o600)
	host, _ := os.Hostname()
	record := Record{PID: pid, SessionID: sessionID, Name: name, CWD: cwd, Kind: "chat", Socket: socket, StartedAt: time.Now().UTC(), Host: host, State: "idle"}
	server := &Server{root: root, record: record, listener: listener, done: make(chan struct{})}
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

func (s *Server) serve(deliver DeliverFunc) {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn, deliver)
	}
}

func (s *Server) handle(conn net.Conn, deliver DeliverFunc) {
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
	queued, err := deliver(request.Text, request.From)
	if err != nil {
		code := "delivery_failed"
		if errors.Is(err, ErrQueueFull) {
			code = "queue_full"
		}
		_ = json.NewEncoder(conn).Encode(Response{Error: code})
		return
	}
	_ = json.NewEncoder(conn).Encode(Response{OK: true, Queued: queued})
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
		if !processExists(record.PID) {
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
	if !processExists(record.PID) {
		return Response{}, ErrNotOnline
	}
	conn, err := net.DialTimeout("unix", record.Socket, 2*time.Second)
	if err != nil {
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
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error == "queue_full" {
		return response, ErrQueueFull
	}
	if !response.OK {
		return response, fmt.Errorf("delivery failed: %s", response.Error)
	}
	return response, nil
}

var ErrNotOnline = errors.New("session process is not online")

func Resolve(records []Record, selector string) (Record, error) {
	selector = strings.TrimSpace(selector)
	var matches []Record
	for _, record := range records {
		if record.SessionID == selector || strings.HasPrefix(record.SessionID, selector) || strings.EqualFold(record.Name, selector) {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return Record{}, ErrNotOnline
	}
	if len(matches) > 1 {
		return Record{}, ErrAmbiguous
	}
	return matches[0], nil
}

var ErrAmbiguous = errors.New("session selector matches multiple processes")
