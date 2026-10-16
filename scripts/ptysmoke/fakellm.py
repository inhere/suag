#!/usr/bin/env python3
"""Fake OpenAI-compatible chat model that streams SSE, for suga smoke tests.

Modes (--mode):
  tool  every user message first gets a shell_exec call (--command), and the
        request after the tool result gets the text --reply. (default)
  text  every request gets the text --reply.
  edit  every user message gets file_grep (pattern SMOKE_TARGET), then after
        its result file_edit on target.txt ("SMOKE_TARGET old" -> "... new"),
        then the text --reply.
  memory  every user message gets shell_exec (--command), then memory add
        (kind feedback, title "Smoke rule"), then the text --reply; a user
        message containing "直接" skips the shell_exec.
  adddir  every user message first gets file_read (--read-path), then
        file_write (--write-path with --write-body), then the text --reply:
        the smoke for --add-dir, where both paths live outside the workspace.
        A user message containing "只读" or "只写" narrows it to that one call,
        so a smoke can drive the read and the write of one turn separately.

--delay seconds are slept before each response, so a test can type while a
turn is running. --chunk splits a streamed text reply into pieces of that many
characters with --chunk-delay seconds between them (a reply that arrives slowly,
one line at a time), and --lines/--wide turn the text reply into a long answer of
that many lines, each padded to that many columns (a line wider than the
terminal window). --fail-first N answers the first N requests with HTTP 503;
--overload-first M then answers the next M with a 200 carrying only a temporary
API error (server_is_overloaded: an SSE error event, or a JSON error body for a
non-streamed call); --break-first M then streams half of the next M responses'
text and drops the connection (a broken stream), for the retry smoke. --fail-from N
[--fail-count M] fails from the Nth request on, M times (M <= 0: every later
request), which lets the first steps of a turn run and then ends it with a
failure (the interrupted-turn replay smoke). --background makes
the tool-mode shell_exec call a background task; a user message that is a
background task notice or reminder ("[background task") gets the text reply;
--watch-paths a,b adds watch_paths (a watch task). --agent makes the
tool-mode call the agent tool (description "smoke survey", with background
when --background); a sub-agent's request (its system prompt names a "suga
sub-agent") greps SMOKE once, then reports "SUBREPORT: ..." after
--child-delay seconds more. A request with "stream": false gets one JSON completion
instead of SSE (the compaction summary). --prompt-tokens sets the reported prompt usage (the idle
consolidation compares it with the context threshold). Each request is
logged as one JSON line on stdout: "user" is the last user message, "seen" is one
token per message in order, which is what a smoke checks when it has to see more
than the last message ("user:<head>" of the text, "a" or "a:file_write" for an
assistant message and its tool calls, "tool:<head>" for a tool result), and "tail"
the last few messages as {"role", "text"} (a steer a person inserted into the
running turn shows up there, after the tool result).

  python3 -I fakellm.py --port 18795 --mode tool --delay 1
"""
import argparse
import http.server
import json
import sys
import threading
import time


def message_token(m):
    """One short token for one request message, so a smoke can see what the
    model received and in what order: "user:<head>", "a:file_write",
    "tool:edited ...". The provider sends role=tool messages without a name, so
    the head of their content is what identifies them (openai.go builds "role",
    "content", "tool_call_id" only)."""
    role = str(m.get("role") or "?")
    if role == "assistant":
        names = [c.get("function", {}).get("name", "?") for c in (m.get("tool_calls") or [])]
        return "a:" + ",".join(names) if names else "a"
    head = " ".join(str(m.get("content") or "").split())[:40]
    return "%s:%s" % (role, head)


def last_index(messages, role):
    """The index of the last message with role (-1 when there is none)."""
    for i in range(len(messages) - 1, -1, -1):
        if messages[i].get("role") == role:
            return i
    return -1


def message_tail(messages, count=4, limit=90):
    """The last few messages as {"role", "text"}, so a smoke can see what a
    request actually carried: the tool result of the step and any message a
    person inserted into the running turn (a steer) come after it."""
    out = []
    for m in messages[-count:]:
        content = m.get("content")
        if isinstance(content, list):
            content = " ".join(str(part.get("text", "")) for part in content if isinstance(part, dict))
        text = " ".join(str(content or "").split())
        calls = m.get("tool_calls") or []
        if calls:
            names = ",".join(str((c.get("function") or {}).get("name", "?")) for c in calls)
            text = (text + " tool_calls:" + names).strip()
        out.append({"role": m.get("role"), "text": text[:limit]})
    return out


def build_reply(args):
    """The text the model answers with: --reply, or a long answer of --lines lines
    of --wide columns each when the short one should not make the point."""
    if args.mode != "text" or args.lines <= 1:
        return args.reply
    out = []
    for i in range(args.lines):
        line = "%s-%02d " % (args.reply, i + 1)
        out.append(line.ljust(args.wide, ".") if args.wide > len(line) else line)
    return "\n".join(out)


def chunked(chunks, size):
    """Splits every text chunk into pieces of size characters: the reply then
    streams in small steps, so a scenario can type while it arrives."""
    out = []
    for chunk in chunks:
        text = None
        choices = chunk.get("choices") or []
        if len(choices) == 1:
            delta = choices[0].get("delta") or {}
            if set(delta) == {"content"}:
                text = delta["content"]
        if not text or len(text) <= size:
            out.append(chunk)
            continue
        for i in range(0, len(text), size):
            out.append({"choices": [{"delta": {"content": text[i:i + size]}}]})
    return out


def make_handler(args):
    served = [0]  # requests so far (the server runs handlers in threads)
    lock = threading.Lock()

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            user_text = next((m.get("content") or "" for m in reversed(body["messages"])
                              if m.get("role") == "user"), "")
            # "user" is the last user message: what a smoke can check the model
            # actually received (e.g. a pasted multi-line text as one message).
            print(json.dumps({"path": self.path, "model": body.get("model"), "stream": body.get("stream"),
                              "last_role": body["messages"][-1].get("role"),
                              "user": user_text,
                              "seen": [message_token(m) for m in body["messages"]],
                              "tail": message_tail(body["messages"])}), flush=True)
            system = " ".join(str(m.get("content")) for m in body["messages"] if m.get("role") == "system")
            child = "suga sub-agent" in system
            # --child-delay slows the sub-agent's report (after its grep).
            reporting = child and body["messages"][-1].get("role") == "tool"
            time.sleep(args.delay + (args.child_delay if reporting else 0))
            with lock:
                served[0] += 1
                n = served[0]
            if n <= args.fail_first:
                self.refuse()
                return
            # --fail-from N [--fail-count M]: fail from the Nth request on, M
            # times (M <= 0: every later request), so the first steps of a turn
            # can run and the turn then ends with a failure.
            if (args.fail_from and n >= args.fail_from
                    and (args.fail_count <= 0 or n < args.fail_from + args.fail_count)):
                self.refuse()
                return
            # --overload-first M: the next M requests answer with a temporary API
            # error only (an overload), the shape responses/openai report inside
            # the stream (or in a 200 body without a stream).
            after_failures = n - args.fail_first
            if after_failures <= args.overload_first:
                self.overload(body.get("stream"))
                return
            broken = after_failures <= args.overload_first + args.break_first
            last = body["messages"][-1]
            # The step's tool result is already in this request: the model answers
            # instead of calling the tool again. Reading only the last message
            # would call it again when a message inserted into the turn (a steer)
            # follows the result.
            tool_done = last_index(body["messages"], "tool") > last_index(body["messages"], "assistant")
            # Tool results since the last user message: the step of this turn.
            step = 0
            for m in reversed(body["messages"]):
                if m.get("role") == "user":
                    break
                if m.get("role") == "tool":
                    step += 1
            edit_calls = [("file_grep", {"pattern": "SMOKE_TARGET"}),
                          ("file_edit", {"path": "target.txt", "old_string": "SMOKE_TARGET old",
                                         "new_string": "SMOKE_TARGET new"})]
            adddir_calls = [("file_read", {"path": args.read_path}),
                            ("file_write", {"path": args.write_path, "content": args.write_body})]
            memory_calls = [("shell_exec", {"command": args.command}),
                            ("memory", {"action": "add", "kind": "feedback", "title": "Smoke rule",
                                        "body": "smoke memory body"})]
            if args.mode == "adddir":
                # 只读/只写 asks for one of the two calls only (a smoke that needs
                # one of them to meet a question of its own first).
                if "只读" in str(user_text):
                    adddir_calls = adddir_calls[:1]
                elif "只写" in str(user_text):
                    adddir_calls = adddir_calls[1:]
            if args.mode == "memory" and "直接" in str(user_text):
                memory_calls = memory_calls[1:]
            if child:
                # A sub-agent: one grep, then the report.
                if last.get("role") != "tool":
                    call = {"index": 0, "id": "call_%d" % time.time_ns(),
                            "function": {"name": "file_grep", "arguments": json.dumps({"pattern": "SMOKE"})}}
                    chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
                else:
                    chunks = [{"choices": [{"delta": {"content": "SUBREPORT: SMOKE is in target.txt"}}]}]
            elif args.mode == "memory" and step < len(memory_calls):
                name, arguments = memory_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "adddir" and step < len(adddir_calls):
                name, arguments = adddir_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "edit" and step < len(edit_calls):
                name, arguments = edit_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "tool" and args.agent and not tool_done and "[background task" not in str(user_text):
                agent_args = {"description": "smoke survey", "prompt": "Where is SMOKE? Report file:line."}
                if args.background:
                    agent_args["background"] = True
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": "agent", "arguments": json.dumps(agent_args)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "tool" and not tool_done and "[background task" not in str(user_text):
                shell_args = {"command": args.command}
                if args.background:
                    shell_args["background"] = True
                if args.watch_paths:
                    shell_args["watch_paths"] = args.watch_paths.split(",")
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": "shell_exec", "arguments": json.dumps(shell_args)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            else:
                chunks = [{"choices": [{"delta": {"content": build_reply(args)}}]}]
            usage = {"prompt_tokens": args.prompt_tokens, "completion_tokens": 2,
                     "total_tokens": args.prompt_tokens + 2}
            chunks = chunked(chunks, args.chunk) if args.chunk > 0 else chunks
            if not body.get("stream"):
                # Non-streaming calls (e.g. the compaction summary) get one JSON body.
                delta = chunks[0]["choices"][0]["delta"]
                message = {"role": "assistant", "content": delta.get("content")}
                if "tool_calls" in delta:
                    message["tool_calls"] = [dict(c, type="function") for c in delta["tool_calls"]]
                finish = "tool_calls" if "tool_calls" in delta else "stop"
                raw = json.dumps({"id": "fake", "object": "chat.completion", "usage": usage,
                                  "choices": [{"index": 0, "message": message, "finish_reason": finish}]}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
                return
            chunks.append({"choices": [], "usage": usage})
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            if broken:
                # Half of the text, then hang up without ending the body.
                text = args.reply[: max(1, len(args.reply) // 2)]
                chunks, self.close_connection = [{"choices": [{"delta": {"content": text}}]}], True
            for chunk in chunks + ([] if broken else ["[DONE]"]):
                if args.chunk_delay > 0 and isinstance(chunk, dict) and chunk.get("choices"):
                    time.sleep(args.chunk_delay)
                data = "data: " + (chunk if isinstance(chunk, str) else json.dumps(chunk)) + "\n\n"
                raw = data.encode()
                self.wfile.write(b"%x\r\n%s\r\n" % (len(raw), raw))
                self.wfile.flush()
            if not broken:
                self.wfile.write(b"0\r\n\r\n")

        def overload(self, stream):
            """Answer 200 with only a temporary API error (an overload): as an SSE
            error event when streamed, as a JSON error body otherwise. Both are
            the shapes the providers report an overload in, and both must be
            retried instead of failing the turn."""
            message = "Our servers are currently overloaded. Please try again later."
            if stream:
                raw = ('data: %s\n\n' % json.dumps({"error": {"code": "server_is_overloaded",
                                                         "message": message}})).encode()
                ctype = "text/event-stream"
            else:
                raw = json.dumps({"error": {"code": "server_is_overloaded", "message": message}}).encode()
                ctype = "application/json"
            self.send_response(200)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def refuse(self):
            """Answer 503: the shape the retry policy treats as transient."""
            raw = b'{"error":{"message":"overloaded"}}'
            self.send_response(503)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def log_message(self, *a):
            pass

    return Handler


class QuietServer(http.server.ThreadingHTTPServer):
    """Threads: stream and non-stream calls use separate client connections,
    and a kept-alive idle connection must not block the other one. A client
    that hangs up (Ctrl-C, a preempted idle consolidation) is not an error."""
    daemon_threads = True

    def handle_error(self, request, client_address):
        if isinstance(sys.exc_info()[1], (BrokenPipeError, ConnectionResetError)):
            return
        super().handle_error(request, client_address)


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--port", type=int, default=18795)
    p.add_argument("--mode", choices=["tool", "text", "edit", "memory", "adddir"], default="tool")
    p.add_argument("--delay", type=float, default=1.0)
    p.add_argument("--command", default="echo SMOKE_OK")
    p.add_argument("--reply", default="完成")
    p.add_argument("--lines", type=int, default=0, help="text mode: answer with this many lines")
    p.add_argument("--wide", type=int, default=0, help="text mode with --lines: pad every line to this many columns")
    p.add_argument("--chunk", type=int, default=0, help="split a streamed text reply into pieces of this many characters")
    p.add_argument("--chunk-delay", type=float, default=0.05, help="seconds between two chunks of a streamed reply")
    p.add_argument("--prompt-tokens", type=int, default=10)
    p.add_argument("--background", action="store_true", help="tool mode: shell_exec with background: true")
    p.add_argument("--watch-paths", default="", help="tool mode with --background: comma-separated watch_paths")
    p.add_argument("--read-path", default="", help="adddir mode: file to read (outside the workspace)")
    p.add_argument("--write-path", default="", help="adddir mode: file to write (under a :rw added directory)")
    p.add_argument("--write-body", default="SMOKE_ADD_DIR_WRITE", help="adddir mode: content of the written file")
    p.add_argument("--agent", action="store_true", help="tool mode: call the agent tool instead of shell_exec")
    p.add_argument("--child-delay", type=float, default=0, help="extra seconds before each sub-agent response")
    p.add_argument("--fail-first", type=int, default=0, help="answer the first N requests with HTTP 503")
    p.add_argument("--break-first", type=int, default=0, help="then break the next M streams after half the text")
    p.add_argument("--overload-first", type=int, default=0, help="after --fail-first: answer the next M requests with a temporary API error only")
    p.add_argument("--fail-from", type=int, default=0, help="answer requests N and later with HTTP 503 (0 = off)")
    p.add_argument("--fail-count", type=int, default=0, help="--fail-from: how many requests fail (0 = all of them)")
    args = p.parse_args()
    server = QuietServer(("127.0.0.1", args.port), make_handler(args))
    print(json.dumps({"listening": args.port}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        sys.exit(0)


if __name__ == "__main__":
    main()
