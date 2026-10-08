package process

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func shortProcessRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("tmp", "s6proc")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "p-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestServerPingDeliverListAndCleanup(t *testing.T) {
	root := shortProcessRoot(t)
	delivered := make(chan string, 2)
	server, err := Start(root, "session-a1", "chat title", t.TempDir(), func(text, from string) (bool, error) {
		delivered <- from + ":" + text
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record := server.Record()
	if got, err := List(root); err != nil || len(got) != 1 || got[0].SessionID != record.SessionID {
		t.Fatalf("list did not return live registration: %#v err=%v", got, err)
	}
	response, err := Send(record, Request{Type: "ping"})
	if err != nil || !response.OK {
		t.Fatalf("ping failed: %#v err=%v", response, err)
	}
	response, err = Send(record, Request{Text: "hello", From: "gofer"})
	if err != nil || !response.OK || !response.Queued {
		t.Fatalf("delivery failed: %#v err=%v", response, err)
	}
	if got := <-delivered; got != "gofer:hello" {
		t.Fatalf("unexpected delivered payload %q", got)
	}
	if err := server.SetBusy(true); err != nil {
		t.Fatalf("set busy state: %v", err)
	}
	if got, err := List(root); err != nil || len(got) != 1 || got[0].State != "busy" {
		t.Fatalf("busy state was not published: %#v err=%v", got, err)
	}
	if err := server.SetBusy(false); err != nil {
		t.Fatalf("set idle state: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, itoa(record.PID)+".json"), record.Socket} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("server cleanup left %s: %v", path, err)
		}
	}
}

func TestListRemovesOnlyDeadRegistrationsAndResolveDetectsAmbiguity(t *testing.T) {
	root := shortProcessRoot(t)
	deadPID := int(^uint(0) >> 1)
	stale := Record{PID: deadPID, SessionID: "stale", Socket: filepath.Join(root, itoa(deadPID)+".sock"), Host: "test"}
	data, _ := json.Marshal(stale)
	stalePath := filepath.Join(root, itoa(deadPID)+".json")
	if err := os.WriteFile(stalePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale.Socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	active := Record{PID: os.Getpid(), SessionID: "active", Socket: filepath.Join(root, itoa(os.Getpid())+".sock"), Host: "test"}
	activeData, _ := json.Marshal(active)
	if err := os.WriteFile(filepath.Join(root, itoa(active.PID)+".json"), activeData, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := List(root)
	if err != nil || len(got) != 1 || got[0].SessionID != "active" {
		t.Fatalf("expected only live process, got %#v err=%v", got, err)
	}
	if _, err := os.Stat(stale.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket was not removed: %v", err)
	}
	_, err = Resolve([]Record{{SessionID: "abc-1", Name: "same"}, {SessionID: "abc-2", Name: "same"}}, "abc")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected prefix collision, got %v", err)
	}
}

func TestListDoesNotRemoveSocketPathOutsideRunRoot(t *testing.T) {
	root := shortProcessRoot(t)
	outside := filepath.Join(root, "keep.me")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := Record{PID: int(^uint(0) >> 1), SessionID: "unsafe", Socket: outside, Host: "old-host"}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(root, itoa(record.PID)+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "preserve" {
		t.Fatalf("untrusted socket path was modified: data=%q err=%v", data, err)
	}
}

func TestSessionLockRejectsLiveOwnerAndReclaimsDeadOwner(t *testing.T) {
	root := shortProcessRoot(t)
	lockRoot := filepath.Join(root, "locks")
	first, err := AcquireSessionLock(lockRoot, "session-lock", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireSessionLock(lockRoot, "session-lock", 0); !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("live owner should block second open, got %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	dead := lockRecord{PID: int(^uint(0) >> 1), Host: first.rec.Host, SessionID: "session-lock", CreatedAt: time.Now().Add(-time.Hour)}
	data, _ := json.Marshal(dead)
	if err := os.WriteFile(filepath.Join(lockRoot, "session-lock.lock"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireSessionLock(lockRoot, "session-lock", 0)
	if err != nil {
		t.Fatalf("dead owner lock should be reclaimed: %v", err)
	}
	_ = second.Close()
}

func TestSeparateProcessesAreAddressableBySession(t *testing.T) {
	root := shortProcessRoot(t)
	spawn := func(sessionID string) (*exec.Cmd, string) {
		t.Helper()
		ready := filepath.Join(root, sessionID+".ready")
		cmd := exec.Command(os.Args[0], "-test.run=TestProcessChildFixture")
		cmd.Env = append(os.Environ(), "SUAG_PROCESS_FIXTURE=1", "SUAG_PROCESS_ROOT="+root, "SUAG_PROCESS_SESSION="+sessionID, "SUAG_PROCESS_READY="+ready)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(ready); err == nil {
				return cmd, ready
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child %s did not register", sessionID)
		return nil, ""
	}
	first, _ := spawn("dual-session-a")
	defer func() { _ = first.Process.Kill(); _ = first.Wait() }()
	second, _ := spawn("dual-session-b")
	defer func() { _ = second.Process.Kill(); _ = second.Wait() }()
	records, err := List(root)
	if err != nil || len(records) != 2 {
		t.Fatalf("expected two independent registrations, records=%#v err=%v", records, err)
	}
	selected, err := Resolve(records, "dual-session-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Send(selected, Request{Text: "targeted", From: "cli"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "dual-session-a.delivered"))
	if err != nil || string(got) != "cli:targeted" {
		t.Fatalf("target session did not receive text: got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "dual-session-b.delivered")); !os.IsNotExist(err) {
		t.Fatalf("other session received targeted delivery: %v", err)
	}
}

func TestProcessChildFixture(t *testing.T) {
	if os.Getenv("SUAG_PROCESS_FIXTURE") != "1" {
		return
	}
	root, sessionID := os.Getenv("SUAG_PROCESS_ROOT"), os.Getenv("SUAG_PROCESS_SESSION")
	server, err := Start(root, sessionID, sessionID, root, func(text, from string) (bool, error) {
		return false, os.WriteFile(filepath.Join(root, sessionID+".delivered"), []byte(from+":"+text), 0o600)
	})
	if err != nil {
		os.Exit(10)
	}
	_ = os.WriteFile(os.Getenv("SUAG_PROCESS_READY"), nil, 0o600)
	for {
		time.Sleep(time.Second)
		_ = server
	}
}

func TestWindowsLongSocketPathFailsWithClearError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows AF_UNIX path limit")
	}
	root := filepath.Join(shortProcessRoot(t), strings.Repeat("long-segment-", 9))
	_, err := Start(root, "sid", "name", root, nil)
	if err == nil || !strings.Contains(err.Error(), "Windows AF_UNIX limit") {
		t.Fatalf("long socket path should fail clearly without fallback: %v", err)
	}
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
