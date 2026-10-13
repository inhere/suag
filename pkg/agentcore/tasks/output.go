package tasks

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// tailLines and tailLineRunes bound the in-memory tail of a task.
	tailLines     = 20
	tailLineRunes = 400
	// maxPendingLine flushes a line without a newline once it is this long
	// (a progress bar redrawn with \r never ends its line).
	maxPendingLine = 64 << 10
)

// lineWriter writes a task's output to its file one cleaned line at a time
// and keeps the last lines in memory. It is the command's Stdout and Stderr
// (the same value, so exec calls Write from one goroutine at a time).
type lineWriter struct {
	mu        sync.Mutex
	file      *os.File
	clean     func([]byte) string
	max       int64
	pending   []byte
	bytes     int64
	lines     int
	truncated bool
	tail      []string
	closed    bool
}

func newLineWriter(file *os.File, clean func([]byte) string, max int64) *lineWriter {
	return &lineWriter{file: file, clean: clean, max: max}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			if len(w.pending) >= maxPendingLine {
				w.emit(w.pending)
				w.pending = w.pending[:0]
			}
			return len(p), nil
		}
		w.emit(w.pending[:i])
		w.pending = w.pending[i+1:]
	}
}

// emit writes one line (without its newline).
func (w *lineWriter) emit(raw []byte) {
	raw = bytes.TrimRight(raw, "\r")
	line := string(raw)
	if w.clean != nil {
		line = w.clean(raw)
	}
	w.lines++
	short := line
	if utf8.RuneCountInString(short) > tailLineRunes {
		short = string([]rune(short)[:tailLineRunes]) + "…"
	}
	w.tail = append(w.tail, short)
	if len(w.tail) > tailLines {
		w.tail = w.tail[len(w.tail)-tailLines:]
	}
	if w.truncated || w.closed {
		return
	}
	if w.bytes+int64(len(line))+1 > w.max {
		w.truncated = true
		return
	}
	n, _ := io.WriteString(w.file, line+"\n")
	w.bytes += int64(n)
}

// finish flushes the last partial line, notes a cut and closes the file.
func (w *lineWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.emit(w.pending)
		w.pending = nil
	}
	if w.closed {
		return
	}
	if w.truncated {
		_, _ = fmt.Fprintf(w.file, "[output cut at %d bytes; %d lines in all]\n", w.max, w.lines)
	}
	_ = w.file.Close()
	w.closed = true
}

func (w *lineWriter) stats() (int64, int, bool, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytes, w.lines, w.truncated, append([]string(nil), w.tail...)
}

// ReadLines returns up to limit lines of a task's output from line offset
// (1-based; values below 1 read from the start), the total number of lines
// written so far and whether more follow. It works while the task runs.
func ReadLines(path string, offset, limit int) (lines []string, total int, more bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	offset = max(offset, 1)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxPendingLine*2)
	for sc.Scan() {
		total++
		if total >= offset && len(lines) < limit {
			lines = append(lines, sc.Text())
		}
	}
	return lines, total, total > offset-1+len(lines), sc.Err()
}

// GrepLines returns the lines (numbered "n: text") of a task's output that
// match pattern (RE2), at most limit, and how many matched in all.
func GrepLines(path, pattern string, limit int) ([]string, int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("pattern: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxPendingLine*2)
	var out []string
	n, matched := 0, 0
	for sc.Scan() {
		n++
		if re.MatchString(sc.Text()) {
			matched++
			if len(out) < limit {
				out = append(out, fmt.Sprintf("%d: %s", n, strings.TrimRight(sc.Text(), " ")))
			}
		}
	}
	return out, matched, sc.Err()
}
