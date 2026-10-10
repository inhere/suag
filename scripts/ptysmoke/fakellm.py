#!/usr/bin/env python3
"""Fake OpenAI-compatible chat model that streams SSE, for suag smoke tests.

Modes (--mode):
  tool  every user message first gets a shell_exec call (--command), and the
        request after the tool result gets the text --reply. (default)
  text  every request gets the text --reply.
  edit  every user message gets file_grep (pattern SMOKE_TARGET), then after
        its result file_edit on target.txt ("SMOKE_TARGET old" -> "... new"),
        then the text --reply.

--delay seconds are slept before each response, so a test can type while a
turn is running. Each request is logged as one JSON line on stdout.

  python3 -I fakellm.py --port 18795 --mode tool --delay 1
"""
import argparse
import http.server
import json
import sys
import time


def make_handler(args):
    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            print(json.dumps({"path": self.path, "stream": body.get("stream"),
                              "last_role": body["messages"][-1].get("role")}), flush=True)
            time.sleep(args.delay)
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
            if args.mode == "edit" and step < len(edit_calls):
                name, arguments = edit_calls[step]
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": name, "arguments": json.dumps(arguments)}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            elif args.mode == "tool" and last.get("role") != "tool":
                call = {"index": 0, "id": "call_%d" % time.time_ns(),
                        "function": {"name": "shell_exec",
                                     "arguments": json.dumps({"command": args.command})}}
                chunks = [{"choices": [{"delta": {"tool_calls": [call]}}]}]
            else:
                chunks = [{"choices": [{"delta": {"content": args.reply}}]}]
            chunks.append({"choices": [], "usage": {"prompt_tokens": 10, "completion_tokens": 2,
                                                    "total_tokens": 12}})
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            for chunk in chunks + ["[DONE]"]:
                data = "data: " + (chunk if isinstance(chunk, str) else json.dumps(chunk)) + "\n\n"
                raw = data.encode()
                self.wfile.write(b"%x\r\n%s\r\n" % (len(raw), raw))
                self.wfile.flush()
            self.wfile.write(b"0\r\n\r\n")

        def log_message(self, *a):
            pass

    return Handler


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--port", type=int, default=18795)
    p.add_argument("--mode", choices=["tool", "text", "edit"], default="tool")
    p.add_argument("--delay", type=float, default=1.0)
    p.add_argument("--command", default="echo SMOKE_OK")
    p.add_argument("--reply", default="完成")
    args = p.parse_args()
    server = http.server.HTTPServer(("127.0.0.1", args.port), make_handler(args))
    print(json.dumps({"listening": args.port}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        sys.exit(0)


if __name__ == "__main__":
    main()
