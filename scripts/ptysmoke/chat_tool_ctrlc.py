#!/usr/bin/env python3
"""Ctrl-C / Esc while a foreground tool call is running.

One script, two behaviours of suga, selected by --steering:

  (default) chat.steering off: a turn keeps the console cooked, so Ctrl-C has to
  reach chat as SIGINT. The turn ends and chat itself exits with its summary; the
  tool process tree dies with it. This is the suga-nrf2 expectation on the platform
  where it can be automated at all.

  --steering: chat.steering on (the default of suga): the turn keeps reading the
  keyboard, so Esc and a single Ctrl-C end the turn while chat stays at the prompt
  (no console Ctrl-C event involved - the path suga-nrf2 fails on under ConPTY), a
  second Ctrl-C within three seconds leaves chat, and the tool is cut short both
  ways. The last check also covers the terminal's output flags: raw input clears
  OPOST, so the turn's own output must still end its lines with CRLF (the marker
  line below), which is what restoreOutputProcessing puts back.

  python3 -I scripts/ptysmoke/chat_tool_ctrlc.py --bin tmp/smoke/suga [--steering]

Reverse verification (make a check fail on purpose): drop syscall.SIGINT from
signal.NotifyContext in cmd/suga/main.go and the steering-off checks fail
("ctrl-c: turn interrupted", "ctrl-c: chat exits with the summary"); remove the
cancelTurn() of watchTurnKeys (chat_turn.go) and the --steering checks
("ctrl-c: turn interrupted", "esc: turn interrupted") fail; drop the
restoreOutputProcessing call of SetBusy and "steering: output keeps CRLF" fails.
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


def scenario_off(s, ws):
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


def scenario_on(s, ws):
    """chat.steering: the turn reads the keyboard, so Esc / a single Ctrl-C end the
    turn and chat stays; two Ctrl-C in the confirmation window leave chat."""
    started = os.path.join(ws, STARTED)
    finished = os.path.join(ws, FINISHED)

    m = s.mark()
    s.check("banner shows model", s.until(r"Suga chat · fake/m1", m))
    s.check("❯ prompt", s.until(r"❯ ", m))

    # 1) A single Ctrl-C ends this turn; chat keeps running.
    m = s.mark()
    s.line("跑一个长命令")
    s.check("tool call shown", s.until(r"shell_exec", m))
    if not wait_file(started, 30):
        s.check("tool started (marker file)", False)
        return
    s.check("tool started (marker file)", True)
    time.sleep(0.5)
    s.send(CTRL_C)
    s.check("ctrl-c: turn interrupted", s.until(r"已中断本轮", m, timeout=15))
    # The prompt after the interrupt (the mark is the one before it: a search from a
    # fresh mark could miss a prompt that already arrived while waiting above).
    s.check("ctrl-c: chat stays at the prompt",
            s.until(r"(?s)已中断本轮.*?❯ ", m, timeout=15))
    time.sleep(0.5)
    s.check("ctrl-c: the tool's command was cut short", not os.path.exists(finished))

    # 2) Esc ends the next turn the same way (no Ctrl-C event involved at all).
    m = s.mark()
    s.line("再来一个长命令")
    s.check("esc: tool call shown", s.until(r"shell_exec", m))
    time.sleep(1.0)
    s.send("\x1b")
    s.check("esc: turn interrupted", s.until(r"已中断本轮", m, timeout=15))
    s.check("esc: chat stays at the prompt",
            s.until(r"(?s)已中断本轮.*?❯ ", m, timeout=15))
    s.check("esc: the tool's command was cut short", not os.path.exists(finished))

    # 3) Two Ctrl-C within the confirmation window leave chat (the first one ended
    # the turn, the second lands at the prompt).
    m = s.mark()
    s.line("第三次")
    s.check("exit: tool call shown", s.until(r"shell_exec", m))
    time.sleep(0.5)
    s.send(CTRL_C)
    time.sleep(0.3)
    s.send(CTRL_C)
    s.check("exit: chat ends", s.until(r"续接：suga chat --resume sess_", m, timeout=20))
    s.check("exit: chat process ended", not s.pump(5.0))
    time.sleep(1.0)
    s.check("exit: the tool's command was cut short", not os.path.exists(finished))


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
    p.add_argument("--steering", action="store_true", help="run the chat.steering on scenario")
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
    steering = "  steering: true\n" if args.steering else "  steering: false\n"
    write_home(home, args.port, tools_extra="  approval: auto\n", extra="chat:\n" + steering)
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
        (scenario_on if args.steering else scenario_off)(s, ws)
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
