package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestChatKeepsRunningWithoutProcessRegistration covers the local socket path limit: when the
// session process cannot register (a SUGA_HOME deep enough that <root>/<pid>.sock exceeds the
// platform limit), chat must keep running and say why it is unreachable instead of exiting
// before the first prompt.
func TestChatKeepsRunningWithoutProcessRegistration(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	// 120 extra characters push the socket path past every platform limit (107 bytes on Linux,
	// 103 on macOS, 100 UTF-16 code units on Windows).
	home := filepath.Join(shortTestHome(t), strings.Repeat("d", 120))
	t.Setenv("SUGA_HOME", home)
	repo, err := store.Open(newTestStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := personal.New(personal.Config{
		Repository:  repo,
		Provider:    mock.NewProvider(mock.Final("unused")),
		ProcessRoot: filepath.Join(home, "run"),
	})
	assert.Require(t, assert.NoErr(t, err))

	var stdout, stderr strings.Builder
	err = Run(context.Background(), runner, Options{}, strings.NewReader(":quit\n"), &stdout, &stderr, nil)
	assert.NoErr(t, err)
	// One Chinese line explains what is lost (`suga send`, gofer delivery) and the way out.
	assert.StrContains(t, stderr.String(), "suga send")
	assert.StrContains(t, stderr.String(), "gofer")
	assert.StrContains(t, stderr.String(), "SUGA_HOME")
	// A failed registration leaves no record and no socket behind.
	entries, dirErr := os.ReadDir(filepath.Join(home, "run"))
	assert.Require(t, assert.NoErr(t, dirErr))
	assert.Len(t, entries, 0)
}
