#!/usr/bin/env python3
"""PTY smoke test for chat's :model inspection, switching and resume.

It configures two fake models, checks that :model lists both, switches to m2,
checks fakellm's request log for the next request's model field, then resumes the
same session and checks both the banner and the next request still use m2. This
is deliberately falsifiable: removing command dispatch, the runtime switch,
the session metadata patch, or the trace event makes a check fail. Reverse
verification: temporarily remove any one of those paths and rerun this scenario;
the corresponding assertion must fail.

Run on Linux/macOS through ``make smoke-chat``; Windows has no PTY equivalent.
"""
import argparse
import json
import os
import re
import select
import shutil
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ptydrive import PtySession  # noqa: E402
from chat_approval import isolate_ws, wait_port, write_home  # noqa: E402


def model_log(proc):
    """Drain fake-model JSON lines and return their model fields."""
    models = []
    fd = proc.stdout.fileno()
    while select.select([fd], [], [], 0)[0]:
        line = proc.stdout.readline()
        if not line:
            break
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if "model" in item:
            models.append(item["model"])
    return models


def wait_models(proc, expected, timeout=20.0):
    """Wait until fakellm has logged expected requests and return all models."""
    end = time.time() + timeout
    if not hasattr(proc, "suga_models"):
        proc.suga_models = []
    while time.time() < end:
        proc.suga_models.extend(model_log(proc))
        if len(proc.suga_models) >= expected:
            return proc.suga_models
        time.sleep(0.05)
    return proc.suga_models


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--bin", required=True)
    p.add_argument("--work", required=True)
    p.add_argument("--port", type=int, default=18845)
    a = p.parse_args()
    home = os.path.abspath(a.work + "/home")
    ws = os.path.abspath(a.work + "/ws")
    shutil.rmtree(a.work, ignore_errors=True)
    os.makedirs(ws)
    isolate_ws(ws)
    provider_extra = """    models:
      m1:
        context_window: 128000
      m2:
        context_window: 64000
        max_output_tokens: 42
"""
    write_home(home, a.port, provider_extra=provider_extra, extra="chat:\n  steering: false\n")
    env = dict(os.environ, SUGA_HOME=home, SUGA_HOOKS="off", TERM="xterm", NO_COLOR="1")
    env.pop("SUGA_CONFIG", None)
    here = os.path.dirname(os.path.abspath(__file__))
    fake = subprocess.Popen([sys.executable, "-I", here + "/fakellm.py", "--port", str(a.port), "--mode", "text", "--reply", "MODEL_REPLY"], stdout=subprocess.PIPE, text=True)
    wait_port(fake)
    first = PtySession([os.path.abspath(a.bin), "chat"], env=env, cwd=ws)
    try:
        mark = first.mark()
        first.check("banner starts with m1", first.until(r"Suga chat · fake/m1", mark))
        mark = first.mark()
        first.line(":model")
        first.check("model list has m1", first.until(r"\* fake/m1", mark))
        first.check("model list has m2", first.until(r"fake/m2", mark))
        mark = first.mark()
        first.line(":model fake/m2")
        first.check("switch accepted", first.until(r"已切换模型：fake/m2", mark))
        mark = first.mark()
        first.line("first request")
        first.check("next request uses m2", wait_models(fake, 1) == ["m2"])
        first.line(":quit")
        time.sleep(0.3)
        session = re.search(r"session id: (sess_\S+)", first.text())
        first.check("session id is printed", session is not None)
        sid = session.group(1) if session else ""
    finally:
        first.close()

    second = PtySession([os.path.abspath(a.bin), "chat", "--resume", sid], env=env, cwd=ws)
    try:
        mark = second.mark()
        second.check("resume banner keeps m2", second.until(r"Suga chat · fake/m2", mark))
        second.line("resumed request")
        second.check("resumed request uses m2", wait_models(fake, 2) == ["m2", "m2"])
        second.line(":quit")
    finally:
        second.close()
    trace = subprocess.run([os.path.abspath(a.bin), "trace", sid, "--json"], capture_output=True, text=True, env=env, timeout=60)
    first.check("trace command succeeds", trace.returncode == 0)
    events = [json.loads(line) for line in trace.stdout.splitlines() if line.strip()]
    switch = next((e for e in events if e.get("kind") == "session" and e.get("name") == "model_switch"), {})
    attrs = switch.get("attrs") or {}
    first.check("trace records model switch", attrs.get("from") == "fake/m1" and attrs.get("to") == "fake/m2")
    fake.terminate()
    fake.wait(timeout=5)
    if first.failed():
        print("failed: " + ", ".join(first.failed()))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
