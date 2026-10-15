//go:build linux

package chat

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// TestTerminalSizeFD: the terminal size is read from the output end when it is a
// terminal (Windows needs a screen-buffer handle for GetConsoleScreenBufferInfo,
// and the input handle fails inside a ConPTY - the bug that kept the line editor at
// its 80-column default). Without a terminal at all there is nothing to read.
func TestTerminalSizeFD(t *testing.T) {
	t.Run("a pty is a terminal on either end", func(t *testing.T) {
		slaveFD := openTestPTY(t)
		slave := os.NewFile(uintptr(slaveFD), "pty")
		assert.Eq(t, slaveFD, terminalSizeFD(slave, slave))
	})

	t.Run("a pipe is not a terminal", func(t *testing.T) {
		assert.Eq(t, -1, terminalSizeFD(bytes.NewReader(nil), &bytes.Buffer{}))
	})
}

// TestRestoreOutputProcessing: raw input (which chat.steering keeps while a turn
// runs) clears OPOST|ONLCR along with the input flags, and chat puts the output
// flags back - otherwise anything written straight to stderr during the turn (a
// hook notice, a warning) would walk down the screen instead of starting a line.
func TestRestoreOutputProcessing(t *testing.T) {
	slave := openTestPTY(t)

	_, err := term.MakeRaw(slave)
	assert.NoErr(t, err)
	raw, err := unix.IoctlGetTermios(slave, getTermiosRequest)
	assert.NoErr(t, err)
	assert.True(t, raw.Oflag&unix.OPOST == 0) // raw mode cleared it

	assert.NoErr(t, restoreOutputProcessing(slave))
	got, err := unix.IoctlGetTermios(slave, getTermiosRequest)
	assert.NoErr(t, err)
	assert.True(t, got.Oflag&unix.OPOST != 0)
	assert.True(t, got.Oflag&unix.ONLCR != 0)
}

// openTestPTY opens a pty (/dev/ptmx plus its /dev/pts/N slave) and returns the
// slave; a host without /dev/ptmx (some sandboxes) skips the test instead of
// failing it.
func openTestPTY(t *testing.T) (slave int) {
	t.Helper()
	master, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no /dev/ptmx:", err)
	}
	t.Cleanup(func() { _ = unix.Close(master) })
	// unlockpt: clear TIOCSPTLCK, then ask for the slave's number.
	assert.NoErr(t, unix.IoctlSetPointerInt(master, unix.TIOCSPTLCK, 0))
	n, err := unix.IoctlGetInt(master, unix.TIOCGPTN)
	assert.NoErr(t, err)
	slave, err = unix.Open(fmt.Sprintf("/dev/pts/%d", n), unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skip("cannot open the pty slave:", err)
	}
	t.Cleanup(func() { _ = unix.Close(slave) })
	return slave
}
