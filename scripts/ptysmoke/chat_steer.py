#!/usr/bin/env python3
"""Steering: inserting a message into a running turn, and ending one turn only.

chat.steering (on by default) keeps the input line available while a turn runs:
Enter inserts a message into the running turn, Esc or a single Ctrl-C end this
turn only. The scenarios (each on its own scratch home and fake model):

  same-turn  a shell_exec runs for a few seconds; the message typed while it runs
             reaches the *same* turn: the request after the tool result carries
             it (fakellm's request log records the last messages of every
             request), the turn ends with one turn event and a steer delivered
             event, and the end-of-turn line counts the message.
  next-turn  the message arrives while the model answers (text mode, a slow
             reply): the answer stands and the message becomes the next turn at
             once (design D3) - the second request carries it as its prompt, the
             trace records it returned (completed_untaken, next_turn).
  abort      Esc ends the turn only: chat goes on, the message waits in the input
             line and runs only when the person submits it (Enter); a single
             Ctrl-C ends a turn the same way, two of them leave chat.
  approval   typing at an approval question is the answer, never an inserted
             message: no 插话 line, no request carrying that text, no steer event.
  screen     a long, overwide reply streams in slowly while a message is typed:
             the minimal VT screen model (ptydrive.Screen) says the reply keeps
             its rows, whole, and the input line stays one line of its own - the
             check for "only complete lines leave" (r3 M1), for the terminal
             width the line is laid out with, and for the waiting line being off.
  hook       a PostToolUse hook writes a line straight to the terminal and one
             to stderr (chat prints the latter as a notice) while a message is
             typed: the input line keeps its place (task 4/N4, r3 M1).
  off        chat.steering: false keeps the old behaviour: no ↳ prompt during the
             turn, nothing is read, a single Ctrl-C still ends the chat.

  python3 -I scripts/ptysmoke/chat_steer.py --bin tmp/smoke/suga

Reverse verification (each check must fail on purpose), verified 2026-10-15 in a
Linux container:
- drop the Runner.Steer call in turnInput.insert: same-turn fails (the request
  after the tool result carries no inserted message) and next-turn fails.
- run the turn with the chat's context instead of a derived one, or drop the
  cancel in turnInput.endTurn: abort fails (the turn ends the chat, no 已中断本轮).
- drop PrependLine from putSteersBack: abort fails (the message is gone from the
  input line, Enter runs nothing and the second request never comes).
- with steering on, drop shell_exec's cmd.Cancel: abort fails, the tool's sleep
  keeps the turn running (in a PTY Ctrl-C no longer reaches the foreground
  process group; the turn's context is what ends the tool).
- drop the generation check in turnInput.handle: nothing here fails, it is a race
  guard (its unit tests are in internal/chat/steering_test.go).
- whole lines off (LiveText.SetWholeLines left a no-op): screen fails, the reply's
  partial line is written next to the input line and the model's rows split.
- no SetSize (the line editor back at its 80-column default): screen fails, the
  rows of a reply wrapped at the pty's 60 columns are counted wrongly.
- no restoreOutputProcessing (OPOST|ONLCR left off in raw mode): hook fails, the
  line the hook wrote straight to the terminal keeps its column and the input line
  is repainted into it.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fakellm  # noqa: E402
from ptydrive import CTRL_C, PtySession, Screen  # noqa: E402
from chat_approval import isolate_ws, wait_port, write_home  # noqa: E402
from chat_observe import trace_events  # noqa: E402

STEER_PROMPT = r"↳ "
# The prompt as the text it is (an exact row of the screen model).
STEER = "↳ "
PROMPT = "❯ "
STARTED = "steer-tool-started"
# The tool whose result a scenario waits for (the model is asked again after it).
QUICK_TOOL = "touch {marker}; sleep 4; echo TOOL_DONE"
# The tool that keeps running until the turn is ended.
SLOW_TOOL = "touch {marker}; sleep 60; echo TOOL_DONE"
# The pty window the screen scenario runs in: each reply line is wider than it, so
# the terminal wraps the line and the chat has to know that width (SetSize).
PTY_COLS = 60
LONG = "STEER_LONG"
LONG_LINES = 3
# Not a multiple of PTY_COLS, and neither is a line with the ▣ prefix: a line that
# ends exactly at the right edge makes the row count ambiguous (a terminal holds the
# wrap back until the next character), which is not what this scenario is about.
LONG_WIDE = 175
# The long reply of the screen scenario, built by fakellm itself (--lines/--wide),
# so the expectation cannot drift from the answer.
LONG_REPLY = fakellm.build_reply(argparse.Namespace(mode="text", reply=LONG, lines=LONG_LINES, wide=LONG_WIDE))
# What the hook scenario's hook writes while the turn runs: a line straight to the
# terminal (a bare newline) and one to stderr, which chat prints as a notice.
HOOK_TTY = "HOOK_TTY_LINE"
HOOK_TTY2 = "HOOK_TTY_NEXT"
HOOK_NOTICE = "HOOK_NOTICE_LINE"
HOOK_SCRIPT = '''import json, sys
event = json.loads(sys.stdin.readline() or "{}")
name = event.get("hook_event_name") or event.get("name") or event.get("Name") or ""
if name == "PostToolUse":
    # A bare newline written to the terminal straight: the chat's raw input mode
    # has to keep the output post-processing on (OPOST|ONLCR), or the line keeps
    # its column and the input line is repainted into it.
    with open("/dev/tty", "w", encoding="utf-8") as tty:
        tty.write(%r + "\\n" + %r + "\\n")
    sys.stderr.write(%r + "\\n")
print(json.dumps({"hookSpecificOutput": {"additionalContext": "SMOKE hook context"}}))
''' % (HOOK_TTY, HOOK_TTY2, HOOK_NOTICE)


def wrapped_rows(text, cols, prefix=""):
    """The screen rows a text of whole lines takes on a cols column screen: the
    prefix marks the start of the model message (its first line only, like LiveText
    prints it) and the terminal wraps every line at the window width."""
    rows = []
    for line in (prefix + text).split("\n"):
        rows += [line[i:i + cols] for i in range(0, len(line), cols)]
    return rows


class FakeLog:
    """The request log of fakellm.py: one JSON object per model request.

    Every request records its last few messages as {"role", "text"}: how a
    scenario tells whether an inserted message reached the same turn (it shows up
    right after the tool result) or became a turn of its own (it is the prompt)."""

    def __init__(self, proc):
        self._requests = []
        self._lock = threading.Lock()
        self._thread = threading.Thread(target=self._read, args=(proc,), daemon=True)
        self._thread.start()

    def _read(self, proc):
        for line in proc.stdout:
            try:
                request = json.loads(line)
            except ValueError:
                continue
            if "listening" in request:
                continue
            with self._lock:
                self._requests.append(request)

    def requests(self):
        with self._lock:
            return list(self._requests)

    def wait(self, count, timeout=30):
        """Waits for count requests and returns as many as arrived."""
        end = time.time() + timeout
        while time.time() < end:
            got = self.requests()
            if len(got) >= count:
                return got
            time.sleep(0.05)
        return self.requests()


def user_texts(request):
    """The text of the user messages in a request's tail, in order."""
    return [m.get("text", "") for m in request.get("tail", []) if m.get("role") == "user"]


def last_user(request):
    """The text of the last user message of a request ("" when there is none)."""
    texts = user_texts(request)
    return texts[-1] if texts else ""


class Scenario:
    """What one scenario works with: the session, the fake model's request log,
    the workspace, and the binary and environment a trace check needs."""

    def __init__(self, session, log, ws, binary, env, reply, cols=80):
        self.session, self.log, self.ws, self.binary, self.env = session, log, ws, binary, env
        self.reply = reply
        self.cols = cols

    def check(self, name, ok):
        return self.session.check(name, ok)

    def trace(self):
        """The session's trace events; leave the chat first (call it last)."""
        session_id = self.exit_chat()
        self.check("session id", bool(session_id))
        return trace_events(self.binary, self.env, session_id) if session_id else []

    def exit_chat(self):
        """Two Ctrl-C in a row leave chat; returns the session id."""
        m = self.session.mark()
        time.sleep(0.3)
        self.session.send(CTRL_C)
        time.sleep(0.3)
        self.session.send(CTRL_C)
        self.session.until(r"续接：suga chat --resume sess_", m)
        found = re.search(r"--resume (sess_\S+)", self.session.text(m))
        return found.group(1) if found else ""

    def wait_file(self, name, seconds):
        path = os.path.join(self.ws, name)
        end = time.time() + seconds
        while time.time() < end:
            if os.path.exists(path):
                return True
            time.sleep(0.1)
        return False


def same_turn_scenario(c):
    """A message typed while a tool runs reaches the same turn."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("跑一个长命令")
    c.check("tool call shown", s.until(r"⏺ shell_exec", m))
    c.check("the prompt becomes ↳ while the turn runs", s.until(STEER_PROMPT, m))
    if not c.wait_file(STARTED, 30):
        c.check("tool started (marker file)", False)
        return
    c.check("tool started (marker file)", True)

    m = s.mark()
    s.line("只改 README")
    c.check("the message is shown as waiting", s.until(r"\[插话\] 只改 README（等待送达）", m))
    # The tool ends, the model is asked again - with the message in the request.
    c.check("tool result shown", s.until(r"⎿ ok: TOOL_DONE", m))
    c.check("shown as delivered", s.until(r"\[插话\] 只改 README（已送达）", m))
    # Only the answer text: steering keeps repainting the input line next to the
    # streamed answer, the layout T3b/M1 reworks.
    c.check("the turn answers", s.until(c.reply, m))
    c.check("the end-of-turn line counts it", s.until(r"插话 1", m))

    requests = c.log.wait(2)
    c.check("two requests", len(requests) >= 2)
    if len(requests) >= 2:
        c.check("the first request has no inserted message",
                "只改 README" not in " ".join(user_texts(requests[0])))
        tail = requests[1].get("tail", [])
        roles = [msg.get("role") for msg in tail]
        c.check("the second request carries it after the tool result",
                "只改 README" in last_user(requests[1]) and "tool" in roles and roles[-1] == "user")

    events = c.trace()
    c.check("one turn only", len([e for e in events if e.get("kind") == "turn"]) == 1)
    delivered = [e for e in events if e.get("kind") == "steer" and e.get("name") == "delivered"]
    c.check("the trace records the delivery", len(delivered) == 1)
    c.check("it names the keyboard source",
            all((e.get("attrs") or {}).get("source") == "keyboard" for e in delivered))


def next_turn_scenario(c):
    """A message that arrives after the model's answer becomes the next turn."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("先看看")
    c.check("the prompt becomes ↳ while the model answers", s.until(STEER_PROMPT, m))
    # The turn opens its inbox just after it starts; a message typed into that
    # first instant is not taken (design m8) and would become the next turn as
    # well, but the point here is the message that arrives during the request.
    time.sleep(1.0)
    s.line("改成 README")
    c.check("the message is shown as waiting", s.until(r"\[插话\] 改成 README（等待送达）", m))
    # The answer stands, then the message runs as a turn of its own: the second
    # request is what that turn asks. (Steering shows the streamed answer next to
    # the input line it keeps repainting - the layout T3b/M1 reworks -, so only
    # the answer text itself is checked here.)
    c.check("the first answer", s.until(c.reply, m))
    requests = c.log.wait(2)
    c.check("two requests", len(requests) >= 2)
    if len(requests) >= 2:
        c.check("the first request has no inserted message",
                "改成 README" not in " ".join(user_texts(requests[0])))
        c.check("the next turn asks the message", last_user(requests[1]) == "改成 README")
    c.check("chat is back at the prompt", s.until(r"❯ ", m))

    events = c.trace()
    c.check("two turns", len([e for e in events if e.get("kind") == "turn"]) == 2)
    returned = [e for e in events if e.get("kind") == "steer" and e.get("name") == "returned"]
    c.check("the trace records the return", len(returned) == 1)
    attrs = (returned[0].get("attrs") or {}) if returned else {}
    c.check("completed and untaken", attrs.get("reason") == "completed_untaken")
    c.check("handed to the next turn", attrs.get("disposition") == "next_turn")


def abort_scenario(c):
    """Esc (and a single Ctrl-C) end this turn only; the message goes back."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("跑一个长命令")
    c.check("tool call shown", s.until(r"⏺ shell_exec", m))
    if not c.wait_file(STARTED, 30):
        c.check("tool started (marker file)", False)
        return
    c.check("tool started (marker file)", True)
    s.line("别改了")
    c.check("the message is shown as waiting", s.until(r"\[插话\] 别改了（等待送达）", m))

    m = s.mark()
    s.send("\x1b")  # Esc: the lone-Esc decision waits 50ms, so send nothing right after
    time.sleep(0.3)
    c.check("esc ends this turn only", s.until(r"已中断本轮，1 条插话已放回输入行", m, timeout=15))
    c.check("chat stays after esc", not s.contains(r"续接：", m))
    c.check("the message is back in the input line", s.until(r"别改了", m))
    time.sleep(0.5)
    c.check("nothing was run on its own", len(c.log.requests()) == 1)

    # The put-back line is not submitted by itself: Enter runs it as the next turn.
    m = s.mark()
    s.send("\r")
    requests = c.log.wait(2)
    c.check("Enter runs the message", len(requests) >= 2 and last_user(requests[1]) == "别改了")
    c.check("the next turn calls the tool again", s.until(r"⏺ shell_exec", m))
    time.sleep(0.5)
    s.send(CTRL_C)
    c.check("one ctrl-c ends this turn only", s.until(r"已中断本轮", m, timeout=15))
    c.check("chat stays after one ctrl-c", not s.contains(r"续接：", m))
    c.exit_chat()


def screen_scenario(c):
    """A long, overwide reply streams slowly while a message is typed: the reply
    keeps its rows, whole, and the input line stays one line of its own (r3 M1)."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("长回答")
    c.check("the prompt becomes ↳ while the model answers", s.until(STEER_PROMPT, m))
    # The waiting line is off while the input line is up (a frame would fight it
    # for the row).
    c.check("no waiting line while the input line is up", not s.contains(r"思考中", m))

    # Type while the reply streams: the chunks arrive slowly (--chunk-delay).
    time.sleep(0.8)
    for part in ["插", "话", "abc"]:
        s.send(part)
        time.sleep(0.4)
    typed = "插话abc"
    c.check("the half line is echoed", s.until(re.escape(typed), m))
    c.check("the reply streams in", s.until(re.escape(LONG + "-01 "), m, timeout=60))
    c.check("its last line arrived", s.until(re.escape("%s-%02d " % (LONG, LONG_LINES)), m, timeout=60))
    c.check("the turn ends", s.until(r"tokens · ", m, timeout=60))
    # The half line comes back on the next prompt (during the turn its prompt was
    # ↳): wait for that redraw before looking at the screen.
    c.check("the half line is back at the prompt", s.until(r"❯ " + re.escape(typed), m, timeout=10))

    # The screen, as a terminal would show it: the rows the reply occupies and the
    # one row the input line lives on.
    screen = Screen(width=c.cols)
    screen.feed(s.text_raw(0))
    lines = screen.screen_lines()
    expected = wrapped_rows(LONG_REPLY, c.cols, prefix="▣ ")
    start = next((i for i, _ in enumerate(lines) if lines[i:i + len(expected)] == expected), None)
    if start is None:
        print("--- screen ---")
        print("\n".join("%3d|%s" % (i, row) for i, row in enumerate(lines)))
        print("--- the rows the reply should occupy ---")
        print("\n".join("   |%s" % row for row in expected))
    c.check("the reply's rows are whole and in place", start is not None)
    typed_rows = [i for i, row in enumerate(lines) if typed in row]
    c.check("the half line is on one row only", len(typed_rows) == 1)
    c.check("the input line is the last row", bool(typed_rows) and typed_rows[-1] == len(lines) - 1)
    if typed_rows:
        # Whichever end of the turn it is: the running one (↳) or the prompt the
        # half line was handed to (❯, design N5).
        c.check("the input line carries its prompt",
                lines[typed_rows[-1]] in (STEER + typed, PROMPT + typed))
    if start is not None:
        # Nothing of the streamed text is left between the reply and the input line:
        # no fragment of it, and no row that is only its fill character.
        after = lines[start + len(expected):]
        residue = [row for row in after if "STEER_LONG" in row or (row and set(row) == {"."})]
        if residue:
            print("--- screen rows after the reply ---")
            print("\n".join("   |%s" % row for row in after))
        c.check("nothing of the reply is left after it", not residue)
    c.exit_chat()


def hook_scenario(c):
    """A hook's notice and the line it writes straight to the terminal arrive
    while a message is typed: the input line keeps its place (N4, r3 M1)."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("跑一下")
    c.check("tool call shown", s.until(r"⏺ shell_exec", m))
    # Type while the hook runs (PostToolUse fires right after the tool result).
    for part in ["插", "一句"]:
        s.send(part)
        time.sleep(0.3)
    typed = "插一句"
    c.check("the hook wrote to the terminal itself", s.until(HOOK_TTY, m, timeout=30))
    c.check("the hook's notice reached chat", s.until(HOOK_NOTICE, m, timeout=30))
    c.check("the half line is echoed", s.until(re.escape(typed), m))
    c.check("no waiting line while the input line is up", not s.contains(r"思考中", m))

    screen = Screen(width=c.cols)
    screen.feed(s.text_raw(0))
    lines = screen.screen_lines()
    typed_rows = [i for i, row in enumerate(lines) if typed in row]
    # A process writing to the terminal itself (the hook) moves the cursor without
    # the line editor knowing, so the input line can be repainted a row lower: what
    # must hold is that it is there, whole, as the last row - and that the hook's own
    # line did not end up glued to chat's notice of it, which is what happens when
    # the raw output has no newline processing left (the erase after it only reaches
    # from the column the write stopped at).
    c.check("the input line is the last row", bool(typed_rows) and typed_rows[-1] == len(lines) - 1)
    if typed_rows:
        c.check("the input line carries its prompt",
                lines[typed_rows[-1]] in (STEER + typed, PROMPT + typed))
    # Two lines written straight to the terminal: with output processing kept on
    # (OPOST|ONLCR) the second starts at column 0; without it, it starts where the
    # first one ended (a staircase).
    c.check("a second line the hook wrote starts at column 0",
            any(row.rstrip() == HOOK_TTY2 for row in lines))
    c.check("the hook's own line stayed a row of its own",
            not any(HOOK_TTY in row and HOOK_NOTICE in row for row in lines))
    if not any(row.rstrip() == HOOK_TTY or HOOK_NOTICE in row for row in lines):
        print("--- screen ---")
        print("\n".join("%3d|%s" % (i, row) for i, row in enumerate(lines)))
    # End the turn only, then leave chat.
    s.send("\x1b")
    c.check("esc ends this turn only", s.until(r"已中断本轮", m, timeout=15))
    c.exit_chat()


def approval_scenario(c):
    """An approval answer is never taken as an inserted message."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("跑一下")
    c.check("the question is asked", s.until(r"允许执行 shell_exec", m))
    s.line("插一句")  # not y/a: the answer, not a steer
    c.check("the answer is used as the answer", s.until(r"用户拒绝执行 shell_exec", m))
    c.check("no inserted message was shown", not s.contains(r"\[插话\]", m))
    c.check("the turn ends", s.until(r"tokens · ", m))
    time.sleep(0.5)
    c.check("no request carried that text",
            all("插一句" not in last_user(r) for r in c.log.requests()))
    events = c.trace()
    c.check("no steer in the trace", not [e for e in events if e.get("kind") == "steer"])


def off_scenario(c):
    """chat.steering: false keeps the old behaviour."""
    s = c.session
    m = s.mark()
    c.check("❯ prompt", s.until(r"❯ ", m))
    m = s.mark()
    s.line("跑一个长命令")
    c.check("tool call shown", s.until(r"⏺ shell_exec", m))
    if not c.wait_file(STARTED, 30):
        c.check("tool started (marker file)", False)
        return
    c.check("tool started (marker file)", True)
    c.check("no ↳ prompt during the turn", not s.contains(STEER_PROMPT, m))
    # Cooked mode: the bytes wait in the tty, nothing reads them, so nothing is
    # inserted into the turn.
    s.send("插一句")
    time.sleep(0.5)
    c.check("typing during the turn inserts nothing", not s.contains(r"\[插话\]", m))
    m = s.mark()
    s.send(CTRL_C)
    c.check("one ctrl-c still ends the chat", s.until(r"续接：suga chat --resume sess_", m))


def run(name, binary, work, port, fake_args, tool, tools_extra, extra, scenario, keep, reply,
        dump=False, cols=None, hooks=False):
    home = os.path.abspath(os.path.join(work, "home"))
    ws = os.path.abspath(os.path.join(work, "ws"))
    shutil.rmtree(home, ignore_errors=True)
    os.makedirs(ws, exist_ok=True)
    isolate_ws(ws)
    if hooks:
        # The hook scenario's command: it writes a line straight to the terminal and
        # one to stderr (chat prints that as a notice), so both arrive while the
        # input line is up. PostToolUse is off unless the config asks for it.
        os.makedirs(home, exist_ok=True)
        hook = os.path.join(home, "hook.py")
        with open(hook, "w", encoding="utf-8") as f:
            f.write(HOOK_SCRIPT)
        extra += ("hooks:\n  command: [%s, -I, %s]\n  post_tool_use:\n    tools: ['*']\n"
                  % (json.dumps(sys.executable), json.dumps(hook)))
    write_home(home, port, tools_extra=tools_extra, extra=extra)
    env = dict(os.environ, SUGA_HOME=home, SUGA_HOOKS="on" if hooks else "off", TERM="xterm", NO_COLOR="1")
    env.pop("SUGA_CONFIG", None)

    here = os.path.dirname(os.path.abspath(__file__))
    fake = subprocess.Popen([sys.executable, "-I", os.path.join(here, "fakellm.py"),
                             "--port", str(port),
                             "--command", tool.format(marker=os.path.join(ws, STARTED)),
                             *fake_args],
                            stdout=subprocess.PIPE, text=True)
    s = None
    try:
        wait_port(fake)
        log = FakeLog(fake)
        # The pty's window size is what the chat reads as the terminal width (and
        # what a terminal would wrap the lines at); only the screen scenarios need a
        # size of their own, the others keep the pty's default.
        s = PtySession([binary, "chat"], env=env, cwd=ws, size=(cols, 24) if cols else None)
        print(f"\n== {name} ==")
        scenario(Scenario(s, log, ws, binary, env, reply, cols=cols or 80))
        if dump:
            print("--- output ---")
            print(s.text())
            print("--- end ---")
        s.close()
    finally:
        if s is not None:
            s.close(grace=0)
        fake.terminate()
        fake.wait(timeout=5)
        if not keep:
            shutil.rmtree(home, ignore_errors=True)
    return [(f"{name}: {check}", ok) for check, ok in (s.results if s else [])]


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bin", default="tmp/smoke/suga", help="suga binary to test")
    p.add_argument("--work", default="tmp/smoke-steer", help="scratch dir (SUGA_HOME and workspace)")
    p.add_argument("--port", type=int, default=18811)
    p.add_argument("--keep", action="store_true", help="keep the scratch homes after the run")
    p.add_argument("--dump", action="store_true", help="print everything the chat wrote, per scenario")
    p.add_argument("--only", default="", help="run one scenario by name only")
    args = p.parse_args()

    binary = os.path.abspath(args.bin)
    # No approval override in the approval scenario: the default is asking, and
    # the question is the subject there.
    auto = "  approval: auto\n"
    # The answer text the model gives in the steering scenarios: short, one
    # chunk, and not a word any prompt of these scenarios uses.
    reply = "STEER_OK"
    scenarios = [
        ("same-turn", ["--mode", "tool", "--delay", "0.2", "--reply", reply], QUICK_TOOL, auto, "",
         same_turn_scenario, {}),
        ("next-turn", ["--mode", "text", "--delay", "3", "--reply", reply], QUICK_TOOL, auto, "",
         next_turn_scenario, {}),
        ("abort", ["--mode", "tool", "--delay", "0.2", "--reply", reply], SLOW_TOOL, auto, "",
         abort_scenario, {}),
        # A long, overwide reply in small chunks: the screen model checks where its
        # lines and the input line ended up. The pty is 60 columns wide, so every
        # reply line is wrapped by the terminal.
        ("screen", ["--mode", "text", "--reply", LONG, "--lines", str(LONG_LINES),
                    "--wide", str(LONG_WIDE), "--chunk", "20", "--chunk-delay", "0.12"],
         QUICK_TOOL, auto, "", screen_scenario, {"cols": PTY_COLS}),
        # A hook writing to the terminal and to stderr while the turn runs.
        ("hook", ["--mode", "tool", "--delay", "4", "--reply", reply], "sleep 2; echo SMOKE_OK", auto, "",
         hook_scenario, {"cols": PTY_COLS, "hooks": True}),
        ("approval", ["--mode", "tool", "--delay", "0.2", "--reply", reply], QUICK_TOOL, "", "",
         approval_scenario, {}),
        ("off", ["--mode", "tool", "--delay", "0.2", "--reply", reply], SLOW_TOOL, auto,
         "chat:\n  steering: false\n", off_scenario, {}),
    ]
    results = []
    for i, (name, fake_args, tool, tools_extra, extra, scenario, opts) in enumerate(scenarios):
        if args.only and name != args.only:
            continue
        results += run(name, binary, os.path.abspath(f"{args.work}-{name}"), args.port + i,
                       fake_args, tool, tools_extra, extra, scenario, args.keep, reply, args.dump, **opts)
    failed = [name for name, ok in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} passed")
    if failed:
        print("failed: " + ", ".join(failed))
        sys.exit(1)


if __name__ == "__main__":
    main()
