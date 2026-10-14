#!/usr/bin/env python3
"""Ctrl-C while a foreground tool call is running: the turn ends and chat exits.

Covers the suga-nrf2 expectation on the platform where it can be automated: the
chat runs in a PTY, the fake model (tool mode) calls shell_exec with a command
that writes a marker, sleeps and writes a second marker; once the tool is
running the script sends Ctrl-C and checks the shell tool was cut short, the
turn is reported as interrupted and chat exits with its summary (the tool's
second marker never appears, so the whole process tree died with the turn).

The Windows twin of this scenario is scripts/wincons (a Windows console cannot
be driven from Python); this one is Linux/macOS only.

  python3 -I scripts/ptysmoke/chat_tool_ctrlc.py --bin tmp/smoke/suga

Reverse verification (make the check fail on purpose): in internal/chat/chat.go
drop the `case ctx.Err() != nil:` branch of the turn error handling (so a
cancelled turn is only reported, chat does not exit); "ctrl-c: chat exits with
the summary" then fails. Or in cmd/suga/main.go drop syscall.SIGINT from
signal.NotifyContext: the turn keeps running and "ctrl-c: turn interrupted"
fails.
"""
import argparse
import os
import shutil
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ptydrive import CTRL_C, PtySession  # noqa: E402
from chat_approval import isolate_ws, wait_port, write_home  # noqa: E402

# The tool writes this marker before it starts sleeping: the script waits for the
# file, not for output (a tool's output only reaches chat when the tool ends).
STARTED = "tool-started"
FINISHED = "tool-finished"


def scenario(s, ws):
    started = os.path.join(ws, STARTED)
    finished = os.path.join(ws, FINISHED)

    m = s.mark()
    s.check("banner shows model", s.until(r"Suga chat · fake/m1", m))
    s.check("❯ prompt", s.until(r"❯ ", m))

    m = s.mark()
    s.line("跑一个长命令")
    s.check("tool call shown", s.until(r"shell_exec", m))
    if not wait_file(started, 30):
        s.check("tool started (marker file)", False)
        return
    s.check("tool started (marker file)", True)
    s.check("tool result not yet shown", not s.contains(r"⎿", m))

    # The tool is running (chat is in its turn, the console cooked): Ctrl-C must
    # end this turn and chat itself, not wait for the command.
    time.sleep(0.5)
    s.send(CTRL_C)
    s.check("ctrl-c: turn interrupted", s.until(r"已中断", m))
    s.check("ctrl-c: chat exits with the summary", s.until(r"续接：suga chat --resume sess_", m))
    # The pty closing means the process ended; the tool must not outlive it.
    s.check("ctrl-c: chat process ended", not s.pump(5.0))
    time.sleep(1.0)
    s.check("ctrl-c: the tool's command was cut short", not os.path.exists(finished))


def wait_file(path, seconds):
    end = time.time() + seconds
    while time.time() < end:
        if os.path.exists(path):
            return True
        time.sleep(0.1)
    return False


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bin", default="tmp/smoke/suga", help="suga binary to test")
    p.add_argument("--work", default="tmp/smoke-tool-ctrlc", help="scratch dir (SUGA_HOME and workspace)")
    p.add_argument("--port", type=int, default=18796)
    p.add_argument("--keep", action="store_true", help="keep the scratch home after the run")
    args = p.parse_args()

    binary = os.path.abspath(args.bin)
    home = os.path.abspath(os.path.join(args.work, "home"))
    ws = os.path.abspath(os.path.join(args.work, "ws"))
    shutil.rmtree(home, ignore_errors=True)
    os.makedirs(ws, exist_ok=True)
    isolate_ws(ws)
    # No approval question: the tool must start on its own, the subject here is
    # the interrupt, not the approval.
    write_home(home, args.port, tools_extra="  approval: auto\n")
    env = dict(os.environ, SUGA_HOME=home, SUGA_HOOKS="off", TERM="xterm", NO_COLOR="1")
    env.pop("SUGA_CONFIG", None)

    here = os.path.dirname(os.path.abspath(__file__))
    command = f"touch {STARTED}; sleep 60; touch {FINISHED}"
    fake = subprocess.Popen([sys.executable, "-I", os.path.join(here, "fakellm.py"),
                             "--port", str(args.port), "--mode", "tool", "--delay", "0.2",
                             "--command", command],
                            stdout=subprocess.PIPE, text=True)
    s = None
    try:
        wait_port(fake)
        s = PtySession([binary, "chat"], env=env, cwd=ws)
        scenario(s, ws)
        s.close()
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate()
        fake.wait(timeout=5)
        if not args.keep:
            shutil.rmtree(home, ignore_errors=True)
    failed = s.failed() if s else ["setup"]
    print(f"\n{len(s.results) - len(failed) if s else 0}/{len(s.results) if s else 0} passed")
    if failed:
        print("failed: " + ", ".join(failed))
        sys.exit(1)


if __name__ == "__main__":
    main()
