#!/usr/bin/env python3
"""End-to-end smoke test: chat shows the "思考中…" waiting line while the model
thinks and erases it before the next output (Linux/macOS).

Starts scripts/ptysmoke/fakellm.py (tool mode with --delay 2, shell_exec
approved automatically) and `suga chat` in a PTY with colors ON (NO_COLOR is
dropped: the line only shows on a colored terminal), sends one prompt and
checks: nothing is shown before the first turn, the line with the elapsed
seconds appears while the model is asked for its first message, the tool call
replaces it, it comes back while the model reads the tool result, and the answer
is printed on a line the hint was erased from. A second chat, configured with
chat.spinner: false, shows no line at all.

  python3 -I scripts/ptysmoke/chat_spinner.py --bin tmp/smoke/suga

Exit status 0 when every check passes. The work dir defaults to tmp/smoke-spinner.
"""
import argparse
import os
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ptydrive import PtySession  # noqa: E402
from chat_approval import isolate_ws, wait_port, write_home  # noqa: E402

HINT = r"思考中… \d+s"
REPLY = "SMOKE_SPINNER_REPLY"
OK = r"⎿ ok: SMOKE_SPINNER_OK"
# shell_exec runs without a question; the danger check still asks.
TOOLS_EXTRA = "  approval: auto\n"
# The line is erased with ESC[2K (carriage return + erase line); ptydrive strips
# those codes from text(), so the raw buffer is needed to see the erase.
ERASE = "\x1b[2K"


def off_chat_config():
    """Top-level config of the second run: the switch turns the line off."""
    return "chat:\n  spinner: false\n"


def scenario(s):
    m = s.mark()
    s.check("banner shows model", s.until(r"Suga chat · fake/m1", m))
    s.check("❯ prompt", s.until(r"❯ ", m))
    s.check("nothing before the first turn", not s.contains("思考中", m))

    m = s.mark()
    s.line("你好")
    s.check("line while waiting for the first message", s.until(HINT, m))
    s.check("tool call line", s.until(r"⏺ shell_exec", m))
    s.check("tool result line", s.until(OK, m))

    m = s.mark()
    s.check("line again while the model reads the result", s.until(HINT, m))
    s.check("answer", s.until(r"▣ " + REPLY, m) and s.until(r"tokens · ", m))

    raw = s.buf[m:].decode("utf-8", "replace")
    hint = raw.rfind("思考中")
    answer = raw.rfind("▣ " + REPLY)
    s.check("the last hint was erased before the answer",
            hint >= 0 and answer > hint and ERASE in raw[hint:answer])


def off_scenario(s):
    """chat.spinner: false shows no waiting line, in a colored terminal."""
    m = s.mark()
    s.check("off: ❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("你好")
    s.check("off: answer", s.until(r"▣ " + REPLY, m) and s.until(r"tokens · ", m))
    s.check("off: no waiting line anywhere", not s.contains("思考中", m))


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bin", default="tmp/smoke/suga", help="suga binary to test")
    p.add_argument("--work", default="tmp/smoke-spinner", help="scratch dir (SUGA_HOME and workspace)")
    p.add_argument("--port", type=int, default=18802)
    p.add_argument("--keep", action="store_true", help="keep the scratch home after the run")
    args = p.parse_args()

    binary = os.path.abspath(args.bin)
    home = os.path.abspath(os.path.join(args.work, "home"))
    off_home = os.path.abspath(os.path.join(args.work, "home-off"))
    ws = os.path.abspath(os.path.join(args.work, "ws"))
    shutil.rmtree(home, ignore_errors=True)
    shutil.rmtree(off_home, ignore_errors=True)
    os.makedirs(ws, exist_ok=True)
    isolate_ws(ws)
    write_home(home, args.port, tools_extra=TOOLS_EXTRA)
    write_home(off_home, args.port, tools_extra=TOOLS_EXTRA, extra=off_chat_config())
    # Colors on: the waiting line needs a terminal without NO_COLOR (the other
    # scenarios keep the default NO_COLOR=1).
    env = dict(os.environ, SUGA_HOME=home, SUGA_HOOKS="off", TERM="xterm")
    env.pop("NO_COLOR", None)
    env.pop("SUGA_CONFIG", None)  # the child reads its own SUGA_HOME config

    here = os.path.dirname(os.path.abspath(__file__))
    fake = subprocess.Popen([sys.executable, "-I", os.path.join(here, "fakellm.py"),
                             "--port", str(args.port), "--mode", "tool", "--delay", "2",
                             "--command", "echo SMOKE_SPINNER_OK", "--reply", REPLY],
                            stdout=subprocess.PIPE, text=True)
    sessions = []
    try:
        wait_port(fake)
        s = PtySession([binary, "chat"], env=env, cwd=ws)
        sessions.append(s)
        scenario(s)
        s.close()

        s_off = PtySession([binary, "chat"], env=dict(env, SUGA_HOME=off_home), cwd=ws)
        sessions.append(s_off)
        off_scenario(s_off)
    finally:
        for session in sessions:
            session.close(grace=0)
        fake.terminate()
        fake.wait(timeout=5)
        if not args.keep:
            shutil.rmtree(home, ignore_errors=True)
            shutil.rmtree(off_home, ignore_errors=True)

    total = sum(len(session.results) for session in sessions)
    failed = [name for session in sessions for name in session.failed()]
    print(f"\n{total - len(failed)}/{total} passed")
    if failed:
        print("failed: " + ", ".join(failed))
        sys.exit(1)


if __name__ == "__main__":
    main()
