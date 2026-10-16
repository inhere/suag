#!/usr/bin/env python3
"""PTY smoke tests for scoped chat approval rules (Linux/macOS).

The scenarios cover a file rule that allows without asking, a matching deny,
scoped ``a`` followed by the persistence transition, and command/path
arguments containing terminal control sequences.  The persistence choice is
left at Enter here; T11 adds the cross-process file-write scenario.

Reverse verification: temporarily restore the old tool-name ``always`` key and
this script's second command must not ask; bypassing ``termtext.Clean`` makes
the raw control-sequence checks fail.
"""
import argparse
import os
import re
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from chat_approval import isolate_ws, wait_port, write_home  # noqa: E402
from ptydrive import PtySession  # noqa: E402

QUESTION = r"允许执行 shell_exec"


def start(binary, work, port, mode="tool", command="echo RULE_OK", write_path=""):
    home = os.path.abspath(os.path.join(work, "home"))
    ws = os.path.abspath(os.path.join(work, "ws"))
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(ws, exist_ok=True)
    isolate_ws(ws)
    extra = "  approval_rules:\n    persist_ask: true\n"
    write_home(home, port, tools_extra=extra)
    env = dict(os.environ, SUGA_HOME=home, SUGA_HOOKS="off", TERM="xterm", NO_COLOR="1")
    env.pop("SUGA_CONFIG", None)
    here = os.path.dirname(os.path.abspath(__file__))
    args = [sys.executable, "-I", os.path.join(here, "fakellm.py"), "--port", str(port),
            "--mode", mode, "--command", command, "--delay", "0.1"]
    if write_path:
        args += ["--write-path", write_path]
    fake = subprocess.Popen(args, stdout=subprocess.PIPE, text=True)
    wait_port(fake)
    return home, ws, env, fake


def rules_file(home, body):
    with open(os.path.join(home, "approval-rules.yaml"), "w", encoding="utf-8") as f:
        f.write("rules:\n" + body)


def check_case(binary, work, port, rule_body, command, expected_question, answer="n"):
    home, ws, env, fake = start(binary, work, port, command=command)
    rules_file(home, rule_body)
    s = None
    try:
        s = PtySession([binary, "chat"], env=env, cwd=ws, size=(100, 30))
        mark = s.mark()
        s.until(r"❯ ", mark)
        s.line("run")
        if expected_question:
            s.check("rule case asks", s.until(QUESTION, mark))
            s.line(answer)
        else:
            s.check("rule case does not ask", not s.until(QUESTION, mark, timeout=0.8))
        s.check("turn completes", s.until(r"完成", mark))
        s.send("\x03\x03")
        s.close()
        return s.results
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate()
        fake.wait(timeout=5)
        shutil.rmtree(work, ignore_errors=True)


def session_scope(binary, work, port):
    home, ws, env, fake = start(binary, work, port, command="echo SCOPED")
    s = None
    results = []
    try:
        s = PtySession([binary, "chat"], env=env, cwd=ws, size=(100, 30))
        mark = s.mark()
        s.until(r"❯ ", mark)
        s.line("first")
        s.check("scoped command prompt", s.until(r"\[a\]本会话总是允许“echo SCOPED”开头的命令", mark))
        s.line("a")
        s.check("persistence prompt", s.until(r"把这条规则写进规则文件", mark))
        s.line("")
        s.check("first call runs", s.until(r"SMOKE_OK|完成", mark))
        mark = s.mark()
        s.line("second")
        s.check("same prefix is not asked again", not s.until(QUESTION, mark, timeout=0.8))
        s.send("\x03\x03")
        s.close()
        results = s.results
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate()
        fake.wait(timeout=5)
        shutil.rmtree(work, ignore_errors=True)
    return results


def control_text(binary, work, port):
    home, ws, env, fake = start(binary, work, port, command="echo\x1b[2KCONTROL")
    s = None
    results = []
    try:
        s = PtySession([binary, "chat"], env=env, cwd=ws, size=(100, 30))
        mark = s.mark(); s.until(r"❯ ", mark); s.line("control")
        s.check("command control bytes are cleaned", s.until(r"命令：echoCONTROL", mark))
        s.check("command escape is not written", not s.contains_raw(re.escape("命令：echo\x1b[2K"), mark))
        s.line("n"); s.until(r"完成", mark)
        s.close(); results = s.results
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate(); fake.wait(timeout=5); shutil.rmtree(work, ignore_errors=True)

    path_work = work + "-path"
    control_path = "dir\x1b[2K/file.txt"
    home, ws, env, fake = start(binary, path_work, port + 1, mode="adddir", write_path=control_path)
    s = None
    try:
        s = PtySession([binary, "chat"], env=env, cwd=ws, size=(100, 30))
        mark = s.mark(); s.until(r"❯ ", mark); s.line("只写")
        s.check("path control bytes are cleaned", s.until(r"路径：.*dir/file.txt", mark))
        s.check("path escape is not written", not s.contains_raw(re.escape("dir\x1b[2K"), mark))
        s.line("n"); s.until(r"完成", mark); s.close(); results += s.results
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate(); fake.wait(timeout=5); shutil.rmtree(path_work, ignore_errors=True)
    return results


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--bin", default="tmp/smoke/suga")
    p.add_argument("--work", default="tmp/smoke-rules")
    p.add_argument("--port", type=int, default=18815)
    args = p.parse_args()
    binary = os.path.abspath(args.bin)
    results = []
    results += check_case(binary, args.work + "-allow", args.port,
                          "  - tool: shell_exec\n    command: echo ALLOW\n    action: allow\n", "echo ALLOW", False)
    results += check_case(binary, args.work + "-deny", args.port + 1,
                          "  - tool: shell_exec\n    command: echo DENY\n    action: deny\n", "echo DENY", False)
    results += session_scope(binary, args.work + "-session", args.port + 2)
    results += control_text(binary, args.work + "-control", args.port + 3)
    failed = [name for name, ok in results if not ok]
    print(f"{len(results) - len(failed)}/{len(results)} passed")
    if failed:
        print("failed: " + ", ".join(failed))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
