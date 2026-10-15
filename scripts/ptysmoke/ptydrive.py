"""Drive an interactive suga process in a pseudo terminal (Linux/macOS).

Lessons built in (see docs/plans/2026-10-09-chat-write-approval-retrospective.md):
  - assertions only look at output produced after a mark, never the whole buffer;
  - wait for an expected pattern before typing the next input (the chat clears
    pending input before an approval question, so early input is discarded);
  - Enter is "\\r" in raw mode, Ctrl-C is "\\x03";
  - check result lines ("⎿ ok: ...") rather than text a prompt also echoes.
"""
import os
import pty
import re
import select
import time

import fcntl
import struct
import termios

ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")
ENTER = "\r"
CTRL_C = "\x03"
CTRL_D = "\x04"
CTRL_U = "\x15"


class PtySession:
    def __init__(self, argv, env=None, cwd=None, size=None):
        """size is the pty's initial (cols, rows); None leaves it unset, so the
        child sees a 0x0 window (suga then falls back to its own 80 columns)."""
        self.buf = b""
        self.results = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:  # child
            if cwd:
                os.chdir(cwd)
            os.execve(argv[0], argv, env or dict(os.environ))
        if size:
            self.resize(size[0], size[1])

    def resize(self, cols, rows):
        """Resize the pty's window, the way a person resizes the terminal: chat
        gets SIGWINCH (or notices on its next poll where there is no signal)."""
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    # --- output ---------------------------------------------------------
    def pump(self, seconds):
        """Read and DROP output for seconds (keeps the child from blocking on
        a full pty). To wait and still check what came, use until."""
        end = time.time() + seconds
        while time.time() < end:
            ready, _, _ = select.select([self.fd], [], [], 0.1)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 4096)
            except OSError:
                return False
            if not data:
                return False
        return True

    def _read_once(self, timeout):
        ready, _, _ = select.select([self.fd], [], [], timeout)
        if ready:
            try:
                data = os.read(self.fd, 4096)
            except OSError:
                return False
            if not data:
                return False
            self.buf += data
        return True

    def drain(self, seconds=0.3):
        """Read pending output INTO the buffer (pump reads and drops it), so a
        contains() check sees what the child printed meanwhile."""
        end = time.time() + seconds
        while time.time() < end:
            if not self._read_once(0.15):
                return

    def mark(self):
        """Position to search from; use it right before sending input."""
        return len(self.buf)

    def text(self, start=0):
        return ANSI.sub("", self.buf[start:].decode("utf-8", "replace"))

    def until(self, pattern, start, timeout=20.0):
        """Wait until pattern (regex) appears in the output after start."""
        end = time.time() + timeout
        while time.time() < end:
            if re.search(pattern, self.text(start)):
                return True
            if not self._read_once(0.2):
                return bool(re.search(pattern, self.text(start)))
        return False

    def contains(self, pattern, start):
        return bool(re.search(pattern, self.text(start)))

    def text_raw(self, start=0):
        """The raw output after start, escape codes kept: text() strips them, so
        it hides color-only output such as the background of an input line."""
        return self.buf[start:].decode("utf-8", "replace")

    def until_raw(self, pattern, start, timeout=20.0):
        """Wait until pattern (regex) appears in the raw output after start."""
        end = time.time() + timeout
        while time.time() < end:
            if re.search(pattern, self.text_raw(start)):
                return True
            if not self._read_once(0.2):
                return bool(re.search(pattern, self.text_raw(start)))
        return False

    def contains_raw(self, pattern, start):
        return bool(re.search(pattern, self.text_raw(start)))

    # --- input ----------------------------------------------------------
    def send(self, s):
        os.write(self.fd, s.encode())

    def line(self, s):
        self.send(s + ENTER)

    # --- checks ---------------------------------------------------------
    def check(self, name, ok):
        self.results.append((name, bool(ok)))
        print(("PASS " if ok else "FAIL ") + name, flush=True)
        return ok

    def close(self, grace=2.0):
        self.pump(grace)
        try:
            os.kill(self.pid, 9)
        except OSError:
            pass
        try:
            os.waitpid(self.pid, 0)
        except OSError:
            pass

    def failed(self):
        return [name for name, ok in self.results if not ok]
