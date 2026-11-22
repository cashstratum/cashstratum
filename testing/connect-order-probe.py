#!/usr/bin/env python3
"""Check the stratum V1 connect sequence a solo pool sends a new miner.

Opens one TCP connection, sends mining.subscribe, waits for its result (up to
5 s), then sends mining.authorize, records every frame the pool sends for a few
seconds and checks that the sequence up to the first mining.notify is exactly:

    subscribe result -> set_difficulty -> authorize result -> set_difficulty
    -> mining.notify

with two mining.set_difficulty frames in that window. Frames that are neither
mining.set_difficulty / mining.notify nor a result for the subscribe or
authorize id (for example client.show_message) are printed as ignored and left
out of the order check. Exits 0 when it is,
1 when the order or count is wrong, 2 on a connection or usage error.

Nothing in test/ or regtest-e2e.sh asserts on wire order, so this probe is
the check for it. Point it only at a pool you operate: a useragent copied
from a marketplace makes its shares look like real rental traffic in the
pool's logs.

    connect-order-probe.py --host 127.0.0.1 --port 3333 \\
        --ua cgminer/4.11.1 --user <payout-address> [--password d=20000]

Pass --ua "" to subscribe with an empty params array.

By default the requests are sent one after the other, as a real miner does.
--pipelined writes both in a single send for stress testing; the pool may then
handle the two requests concurrently, so the order is not guaranteed there.
"""
import argparse
import json
import socket
import sys
import time

SUBSCRIBE_ID = 1
SUBSCRIBE_TIMEOUT = 5.0
AUTHORIZE_ID = 2

EXPECTED = ["subscribe_result", "set_difficulty", "authorize_result",
            "set_difficulty", "notify"]


def classify(msg):
    """Return a short kind name for one decoded frame."""
    if not isinstance(msg, dict):
        return "invalid"
    method = msg.get("method")
    if method == "mining.set_difficulty":
        return "set_difficulty"
    if method == "mining.notify":
        return "notify"
    if method:
        return "other:" + str(method)
    if "result" in msg or "error" in msg:
        if msg.get("id") == SUBSCRIBE_ID:
            return "subscribe_result"
        if msg.get("id") == AUTHORIZE_ID:
            return "authorize_result"
        return "result:id=" + json.dumps(msg.get("id"))
    return "unknown"


class Capture:
    """Buffers newline-delimited JSON frames from one socket."""

    def __init__(self, sock):
        self.sock = sock
        self.buf = b""
        self.frames = []
        self.start = time.monotonic()

    def has(self, kind):
        return any(k == kind for _, k, _ in self.frames)

    def pump(self, seconds, until=None):
        """Read until the window closes, the peer hangs up, or until(kind)
        is true for a frame already captured."""
        deadline = time.monotonic() + seconds
        while not (until and self.has(until)):
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            self.sock.settimeout(remaining)
            try:
                chunk = self.sock.recv(65536)
            except socket.timeout:
                break
            except OSError as exc:
                print("read error: %s" % exc, file=sys.stderr)
                break
            if not chunk:
                break
            self.buf += chunk
            while b"\n" in self.buf:
                line, self.buf = self.buf.split(b"\n", 1)
                line = line.strip()
                if not line:
                    continue
                elapsed = time.monotonic() - self.start
                try:
                    msg = json.loads(line.decode("utf-8", "replace"))
                except ValueError:
                    self.frames.append((elapsed, "unparseable", line[:120]))
                    continue
                self.frames.append((elapsed, classify(msg), msg))

    def finish(self):
        if self.buf.strip():
            self.frames.append((time.monotonic() - self.start, "partial", self.buf[:120]))
            self.buf = b""
        return self.frames


def is_ignored(kind):
    return kind.startswith("other:") or kind.startswith("result:id=")


def describe(kind, msg):
    if kind == "set_difficulty":
        return "set_difficulty %s" % json.dumps(msg.get("params"))
    if kind == "notify":
        params = msg.get("params") or []
        job = params[0] if params else None
        clean = params[-1] if params else None
        return "notify job=%s clean=%s" % (job, clean)
    if kind in ("subscribe_result", "authorize_result"):
        if msg.get("error"):
            return "%s error=%s" % (kind, json.dumps(msg.get("error")))
        return "%s result=%s" % (kind, json.dumps(msg.get("result"))[:80])
    if isinstance(msg, dict):
        return "%s %s" % (kind, json.dumps(msg)[:120])
    return "%s %r" % (kind, msg)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--host", required=True)
    ap.add_argument("--port", required=True, type=int)
    ap.add_argument("--ua", required=True,
                    help='useragent to subscribe with; "" sends subscribe []')
    ap.add_argument("--user", required=True, help="authorize username")
    ap.add_argument("--password", default="x",
                    help="authorize password, e.g. d=20000 (default: x)")
    ap.add_argument("--pipelined", action="store_true",
                    help="send subscribe and authorize in one write instead of "
                         "waiting for the subscribe result; the pool can handle "
                         "pipelined requests concurrently, so the order is not "
                         "guaranteed in this mode")
    ap.add_argument("--seconds", type=float, default=5.0,
                    help="how long to read after authorizing (default: 5)")
    args = ap.parse_args()

    sub_params = [args.ua] if args.ua else []
    try:
        sock = socket.create_connection((args.host, args.port), timeout=10)
    except OSError as exc:
        print("connect %s:%d failed: %s" % (args.host, args.port, exc), file=sys.stderr)
        return 2
    sub_line = (json.dumps({"id": SUBSCRIBE_ID, "method": "mining.subscribe",
                            "params": sub_params}) + "\n").encode()
    auth_line = (json.dumps({"id": AUTHORIZE_ID, "method": "mining.authorize",
                             "params": [args.user, args.password]}) + "\n").encode()
    cap = Capture(sock)
    try:
        if args.pipelined:
            sock.sendall(sub_line + auth_line)
        else:
            sock.sendall(sub_line)
            cap.pump(SUBSCRIBE_TIMEOUT, until="subscribe_result")
            if not cap.has("subscribe_result"):
                print("warning: no subscribe result within %gs, authorizing anyway"
                      % SUBSCRIBE_TIMEOUT, file=sys.stderr)
            sock.sendall(auth_line)
        cap.pump(args.seconds)
        frames = cap.finish()
    except OSError as exc:
        print("send failed: %s" % exc, file=sys.stderr)
        return 2
    finally:
        sock.close()

    print("ua=%r user=%s password=%s" % (args.ua, args.user, args.password))
    for elapsed, kind, msg in frames:
        print("  [%+6.3fs] %s%s" % (elapsed, describe(kind, msg),
                                   "   (ignored)" if is_ignored(kind) else ""))

    # The connect window ends at the first notify; later notifies and vardiff
    # changes are normal traffic and not part of the check.
    window = []
    for _, kind, msg in frames:
        if is_ignored(kind):
            continue
        window.append((kind, msg))
        if kind == "notify":
            break
    kinds = [k for k, _ in window]
    diffs = [(m.get("params") or [None])[0] for k, m in window if k == "set_difficulty"]

    print("order: %s" % " -> ".join(kinds) if kinds else "order: (no frames)")
    print("set_difficulty count: %d (up to the first notify), %d in the whole capture"
          % (len(diffs), sum(1 for _, k, _ in frames if k == "set_difficulty")))
    if diffs:
        print("first diff: %s" % diffs[0])
        print("last diff: %s" % diffs[-1])

    problems = []
    if kinds != EXPECTED:
        problems.append("expected order %s" % " -> ".join(EXPECTED))
    if len(diffs) != 2:
        problems.append("expected 2 set_difficulty, got %d" % len(diffs))
    for kind, msg in window:
        if kind == "authorize_result" and msg.get("result") is not True:
            problems.append("authorize was not accepted: %s" % json.dumps(msg.get("error")))
    if problems:
        for p in problems:
            print("FAIL: %s" % p)
        return 1
    print("PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
