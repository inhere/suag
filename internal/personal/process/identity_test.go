package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// writeReusedPIDRecord registers this test process's PID as if a crashed earlier
// process had owned it: same PID, different start time.
func writeReusedPIDRecord(t *testing.T, root string) Record {
	t.Helper()
	if processStartID(os.Getpid()) == "" {
		t.Skip("process start time is not available on this platform")
	}
	pid := os.Getpid()
	record := Record{PID: pid, SessionID: "reused", Socket: filepath.Join(root, fmt.Sprintf("%d.sock", pid)), Host: "test", ProcStart: "1"}
	data, _ := json.Marshal(record)
	assert.NoErr(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.json", pid)), data, 0o600))
	assert.NoErr(t, os.WriteFile(record.Socket, nil, 0o600))
	return record
}

func TestProcessAliveDetectsReusedPID(t *testing.T) {
	pid := os.Getpid()
	start := processStartID(pid)
	if start == "" {
		t.Skip("process start time is not available on this platform")
	}
	assert.True(t, processAlive(pid, start))
	assert.True(t, processAlive(pid, ""))
	assert.False(t, processAlive(pid, start+"0"))
}

func TestReusedPIDRegistration(t *testing.T) {
	t.Run("list removes it and send reports not online", func(t *testing.T) {
		root := shortProcessRoot(t)
		record := writeReusedPIDRecord(t, root)
		_, err := Send(record, Request{Text: "hi"})
		assert.True(t, errors.Is(err, ErrNotOnline))
		records, err := List(root)
		assert.NoErr(t, err)
		assert.Len(t, records, 0)
		_, err = os.Stat(record.Socket)
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})
	t.Run("start replaces it", func(t *testing.T) {
		root := shortProcessRoot(t)
		writeReusedPIDRecord(t, root)
		server, err := Start(root, "fresh", "fresh", root, nil)
		assert.NoErr(t, err)
		defer server.Close()
		assert.Eq(t, processStartID(os.Getpid()), server.Record().ProcStart)
		_, err = Start(root, "again", "again", root, nil)
		assert.Err(t, err)
	})
}
