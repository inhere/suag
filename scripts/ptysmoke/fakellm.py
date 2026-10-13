#!/usr/bin/env python3
"""Fake OpenAI-compatible chat model that streams SSE, for suag smoke tests.

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

--delay seconds are slept before each response, so a test can type while a
turn is running. --fail-first N answers the first N requests with HTTP 503;
--break-first M then streams half of the next M responses' text and drops
the connection (a broken stream), for the retry smoke. --background makes
the tool-mode shell_exec call a background task; a user message that is a
background task notice or reminder ("[background task") gets the text reply. A request with "stream": false gets one JSON completion
instead of SSE (the compaction summary). --prompt-tokens sets the reported prompt usage (the idle
consolidation compares it with the context threshold). Each request is
logged as one JSON line on stdout.

  python3 -I fakellm.py --port 18795 --mode tool --delay 1
"""
import argparse
import http.server
import json
import sys
import threading
import time


def make_handler(args):
    served = [0]  # requests so far (the server runs handlers in threads)
    lock = threading.Lock()

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            print(json.dumps({"path": self.path, "stream": body.get("stream"),
                              "last_role": body["messages"][-1].get("role")}), flush=True)
            time.sleep(args.delay)
            with lock:
                served[0] += 1
                n = served[0]
            if n <= args.fail_first:
                raw = b'{"error":{"message":"overloaded"}}'
                self.send_response(503)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
                return
            broken = n <= args.fail_first + args.break_first
            last = body["messages"][-1]
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
            memory_calls = [("shell_exec", {"command": args.command}),
                            ("memory", {"action": "add", "kind": "feedback", "title": "Smoke rule",
                                        "body": "smoke memory body"})]
            user_text = next((m.get("content") or "" for m in reversed(body["messages"])
                              if m.get("role") == "user"), "")
            if args.mode == "memory" and "直接" in str(user_text):
                memory_calls = memory_calls[1:]
            if args.mode == "memory" and step < len(memory_calls):
                name, arguments = memory_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "edit" and step < len(edit_calls):
                name, arguments = edit_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "tool" and last.get("role") != "tool" and "[background task" not in str(user_text):
                shell_args = {"command": args.command}
                if args.background:
                    shell_args["background"] = True
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": "shell_exec", "arguments": json.dumps(shell_args)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            else:
                chunks = [{"choices": [{"delta": {"content": args.reply}}]}]
            usage = {"prompt_tokens": args.prompt_tokens, "completion_tokens": 2,
                     "total_tokens": args.prompt_tokens + 2}
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
                data = "data: " + (chunk if isinstance(chunk, str) else json.dumps(chunk)) + "\n\n"
                raw = data.encode()
                self.wfile.write(b"%x\r\n%s\r\n" % (len(raw), raw))
                self.wfile.flush()
            if not broken:
                self.wfile.write(b"0\r\n\r\n")

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
    p.add_argument("--mode", choices=["tool", "text", "edit", "memory"], default="tool")
    p.add_argument("--delay", type=float, default=1.0)
    p.add_argument("--command", default="echo SMOKE_OK")
    p.add_argument("--reply", default="完成")
    p.add_argument("--prompt-tokens", type=int, default=10)
    p.add_argument("--background", action="store_true", help="tool mode: shell_exec with background: true")
    p.add_argument("--fail-first", type=int, default=0, help="answer the first N requests with HTTP 503")
    p.add_argument("--break-first", type=int, default=0, help="then break the next M streams after half the text")
    args = p.parse_args()
    server = QuietServer(("127.0.0.1", args.port), make_handler(args))
    print(json.dumps({"listening": args.port}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        sys.exit(0)


if __name__ == "__main__":
    main()
