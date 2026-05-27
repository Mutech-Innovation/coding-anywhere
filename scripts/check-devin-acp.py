#!/usr/bin/env python3
"""ACP smoke-test for a `devin acp` subprocess.

Background: `devin` 2026.5.26-0 regressed `session/prompt` for Pro accounts —
the call returns JSON-RPC error -32013 ("Permission denied: ... internal
error occurred (trace ID: ...)") and stderr leaks a giveaway line:
`raindrop: 403: Telemetry proxy is only available for free-tier users`.
2026.5.6-12 (and earlier) returns a normal `stopReason: end_turn`.

Run this against any candidate `devin` binary BEFORE switching the
`current` symlink to it. Exits 0 on PASS, 2 on FAIL (regression detected),
1 on transport / unrelated errors.

  scripts/check-devin-acp.py
  scripts/check-devin-acp.py --devin ~/.local/share/devin/cli/_versions/2026.5.26-0/bin/devin
  scripts/check-devin-acp.py --devin /tmp/devin-candidate --cwd /path/to/some/repo
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import threading
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--devin", default=os.environ.get("DEVIN_BIN", "devin"),
                        help="Path to the devin binary to test (default: $DEVIN_BIN, or `devin` on PATH)")
    parser.add_argument("--cwd", default=str(Path.cwd()),
                        help="Working directory passed to session/new (default: $PWD)")
    parser.add_argument("--prompt", default="hi",
                        help="Text sent in session/prompt (default: 'hi')")
    parser.add_argument("--quiet", action="store_true",
                        help="Suppress devin stderr passthrough")
    args = parser.parse_args()

    devin_bin = shutil.which(args.devin) or args.devin
    if not Path(devin_bin).exists():
        print(f"FAIL: devin binary not found: {args.devin}", file=sys.stderr)
        return 1

    proc = subprocess.Popen(
        [devin_bin, "acp"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        bufsize=0,
    )

    saw_free_tier = []  # captured by stderr pumper

    def pump_stderr() -> None:
        while True:
            line = proc.stderr.readline()
            if not line:
                return
            text = line.decode("utf-8", errors="replace").rstrip()
            if "Telemetry proxy is only available for free-tier" in text:
                saw_free_tier.append(text)
            if not args.quiet:
                sys.stderr.write("    stderr> " + text + "\n")
                sys.stderr.flush()

    threading.Thread(target=pump_stderr, daemon=True).start()

    def call(req: dict) -> None:
        proc.stdin.write((json.dumps(req) + "\n").encode())
        proc.stdin.flush()

    def recv_until_id(target_id: int) -> dict:
        while True:
            line = proc.stdout.readline()
            if not line:
                raise SystemExit("FAIL: agent closed stdout before id=%d response" % target_id)
            msg = json.loads(line.decode("utf-8"))
            if msg.get("id") == target_id:
                return msg

    try:
        # 1) initialize
        call({
            "jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {
                "protocolVersion": 1,
                "clientCapabilities": {},
                "clientInfo": {"name": "coding-anywhere", "title": "Coding Anywhere", "version": "check"},
            },
        })
        recv_until_id(1)

        # 2) session/new
        call({
            "jsonrpc": "2.0", "id": 2, "method": "session/new",
            "params": {"cwd": args.cwd, "mcpServers": []},
        })
        new_resp = recv_until_id(2)
        if "error" in new_resp:
            print(f"FAIL: session/new returned error: {new_resp['error']}", file=sys.stderr)
            return 1
        sid = new_resp["result"]["sessionId"]

        # 3) session/prompt — this is what regresses on bad versions
        call({
            "jsonrpc": "2.0", "id": 3, "method": "session/prompt",
            "params": {"sessionId": sid, "prompt": [{"type": "text", "text": args.prompt}]},
        })
        prompt_resp = recv_until_id(3)
    finally:
        try:
            proc.stdin.close()
        except BrokenPipeError:
            pass
        try:
            proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            proc.kill()

    if "error" in prompt_resp:
        err = prompt_resp["error"]
        code = err.get("code")
        msg = err.get("message", "")
        data = err.get("data") or {}
        print(f"FAIL: session/prompt returned error code={code}", file=sys.stderr)
        print(f"      message: {msg}", file=sys.stderr)
        if data:
            print(f"      data: {json.dumps(data)}", file=sys.stderr)
        if saw_free_tier:
            print("      stderr leaked the telltale 'Telemetry proxy is only available for free-tier' "
                  "warning — this is the 2026.5.26-0 regression.", file=sys.stderr)
        return 2

    stop_reason = prompt_resp.get("result", {}).get("stopReason")
    usage = prompt_resp.get("result", {}).get("usage", {})
    print(f"PASS: stopReason={stop_reason!r}, totalTokens={usage.get('totalTokens')}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
