#!/usr/bin/env python3
"""End-to-end smoke test of `suag chat` in a pseudo terminal (Linux/macOS).

Starts scripts/ptysmoke/fakellm.py (tool mode), writes a temporary SUAG_HOME
with tools.allow_write: true, runs `suag chat` in a PTY and checks:
banner with model, ❯ prompt, approval question with the full command,
pre-typed "y⏎" during a turn does not approve, n denies and the turn goes on,
y runs the tool, Ctrl-C at the question only cancels the turn, a stops further
questions, one Ctrl-C at the prompt only hints, two in a row print the resume
command and usage summary,
and piped (non-TTY) input denies write tools.

  make smoke-chat                        # builds tmp/smoke/suag and runs this
  python3 -I scripts/ptysmoke/chat_approval.py --bin tmp/smoke/suag

Exit status 0 when every check passes. The work dir defaults to tmp/smoke.
"""
import argparse
import os
import shutil
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ptydrive import CTRL_C, CTRL_U, PtySession  # noqa: E402

OK = r"⎿ ok: SMOKE_OK"
QUESTION = r"允许执行 shell_exec"


def write_home(home, port, tools_extra="", memory_extra=""):
    """Writes SUAG_HOME/config.yaml. Project memory is forced to the files
    backend: the scratch workspace lives inside this repository, whose
    .gofer/tracker would otherwise select (and call) the real gofer."""
    os.makedirs(home, exist_ok=True)
    with open(os.path.join(home, "config.yaml"), "w", encoding="utf-8") as f:
        f.write(f"""model: fake/m1
providers:
  fake:
    type: openai
    base_url: http://127.0.0.1:{port}/v1
    api_key: k
tools:
  allow_write: true
{tools_extra}memory:
  project:
    backend: files
{memory_extra}""")


def wait_port(proc):
    line = proc.stdout.readline()  # {"listening": port}
    if "listening" not in line:
        raise SystemExit("fake model did not start: " + line)


def scenario(s):
    def turn_end(start):
        return s.until(r"tokens · ", start)

    m = s.mark()
    s.check("banner shows model", s.until(r"Suag chat · fake/m1", m))
    s.check("banner shows write tools on", s.until(r"工具：可读写文件、执行 shell", m))
    s.check("❯ prompt", s.until(r"❯ ", m))

    # Pre-typed y + Enter while the turn runs must not approve.
    m = s.mark(); s.line("跑一下"); time.sleep(0.3); s.line("y")
    s.check("pre-typed y: question still asked", s.until(QUESTION, m))
    s.check("pre-typed y: not executed", not s.contains(OK, m))
    s.line("n")
    s.check("n: denied", s.until(r"用户拒绝执行 shell_exec", m))
    s.check("n: denied tools listed", s.until(r"本轮被拒绝的工具：shell_exec", m))
    s.check("n: turn ends", turn_end(m))
    time.sleep(0.3); s.send(CTRL_U)  # drop the prefilled leftover "y"

    m = s.mark(); s.line("跑一下")
    s.check("y: full command shown", s.until(r"命令：echo SMOKE_OK", m) and s.until(QUESTION, m))
    s.line("y")
    s.check("y: executed", s.until(OK, m) and s.until(r"完成", m))
    s.check("y: answer marked ▣", s.contains(r"▣ 完成", m))
    s.check("y: turn ends", turn_end(m))

    m = s.mark(); s.line("第三次")
    s.check("ctrl-c: question", s.until(QUESTION, m))
    s.send(CTRL_C)
    s.check("ctrl-c: cancelled", s.until(r"已取消", m))
    s.check("ctrl-c: not executed", not s.contains(OK, m))
    time.sleep(0.5)

    m = s.mark(); s.line("继续")
    s.check("a: chat alive, question", s.until(QUESTION, m))
    s.line("a")
    s.check("a: executed", s.until(OK, m) and turn_end(m))
    m = s.mark(); s.line("又一次")
    s.check("a: executed again", s.until(OK, m) and turn_end(m))
    s.check("a: no question again", not s.contains(QUESTION, m))

    # One Ctrl-C at the prompt only drops the typed text and asks for another.
    m = s.mark(); time.sleep(0.3); s.send("abc"); time.sleep(0.2); s.send(CTRL_C)
    s.check("ctrl-c once: hint", s.until(r"再按一次 Ctrl\+C 退出", m))
    s.check("ctrl-c once: chat alive", not s.contains(r"续接：", m))
    # A second Ctrl-C after the window is again only a hint.
    m = s.mark(); time.sleep(3.5); s.send(CTRL_C)
    s.check("ctrl-c late: hint again", s.until(r"再按一次 Ctrl\+C 退出", m))
    s.check("ctrl-c late: chat alive", not s.contains(r"续接：", m))

    m = s.mark(); time.sleep(0.3); s.send(CTRL_C)
    s.check("exit: usage summary", s.until(r"本次 \d+ 轮 · in \d+ / out \d+ tokens", m))
    s.check("exit: resume command", s.until(r"续接：suag chat --resume sess_", m))


def non_tty(binary, env, cwd):
    out = subprocess.run([binary, "chat"], input="跑一下\n:quit\n", capture_output=True,
                         text=True, env=env, cwd=cwd, timeout=60)
    text = out.stdout + out.stderr
    return "标准输入不是终端" in text and "tools.approval: auto" in text


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bin", default="tmp/smoke/suag", help="suag binary to test")
    p.add_argument("--work", default="tmp/smoke", help="scratch dir (SUAG_HOME and workspace)")
    p.add_argument("--port", type=int, default=18795)
    p.add_argument("--keep", action="store_true", help="keep the scratch home after the run")
    args = p.parse_args()

    binary = os.path.abspath(args.bin)
    home = os.path.abspath(os.path.join(args.work, "home"))
    ws = os.path.abspath(os.path.join(args.work, "ws"))
    shutil.rmtree(home, ignore_errors=True)
    os.makedirs(ws, exist_ok=True)
    write_home(home, args.port)
    env = dict(os.environ, SUAG_HOME=home, SUAG_HOOKS="off", TERM="xterm", NO_COLOR="1")

    here = os.path.dirname(os.path.abspath(__file__))
    fake = subprocess.Popen([sys.executable, "-I", os.path.join(here, "fakellm.py"),
                             "--port", str(args.port), "--mode", "tool", "--delay", "1"],
                            stdout=subprocess.PIPE, text=True)
    s = None
    try:
        wait_port(fake)
        s = PtySession([binary, "chat"], env=env, cwd=ws)
        scenario(s)
        s.close()
        s.check("non-tty: write tool denied with hint", non_tty(binary, env, ws))
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
