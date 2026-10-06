#!/usr/bin/env python3
"""stratum_diffprobe.py -- dependency-free stratum V1 client for the
difficulty-policy regtest scenarios (regtest-e2e.sh scenarios 11-14).

Speaks just enough of the stratum V1 JSON-RPC-over-newline-delimited-TCP
protocol to observe the pool's difficulty decisions without hashing
anything: mining.subscribe, mining.authorize, waiting on mining.notify /
mining.set_difficulty, an optional mining.suggest_difficulty, and an
optional single mining.submit with a syntactically valid but unmined
share (a random nonce is already below any assigned difficulty >= 1, so
no proof-of-work is required to prove a reject).

Stdlib only. Python 3.8+ compatible. Never retries a submit and never
loops indefinitely: every socket read is bounded by --timeout.

Emits exactly one JSON object on stdout and exits 0 if the probe ran to
completion (regardless of whether the pool accepted or rejected
anything -- that judgement belongs to the caller), or exits 1 with a
"probe_error" key set if the connection/handshake itself failed.
"""

import argparse
import json
import os
import socket
import sys
import time


def parse_args():
    parser = argparse.ArgumentParser(
        description="Raw stratum V1 probe for difficulty-policy assertions."
    )
    parser.add_argument("--host", default="127.0.0.1", help="pool host (default: 127.0.0.1)")
    parser.add_argument("--port", required=True, type=int, help="pool stratum port")
    parser.add_argument("--user", required=True, help="mining.authorize username")
    parser.add_argument("--password", default="x", help="mining.authorize password (default: x)")
    parser.add_argument("--useragent", default="regtest-diffprobe", help="mining.subscribe useragent")
    parser.add_argument(
        "--suggest",
        type=int,
        default=None,
        help="if given, send mining.suggest_difficulty(N) after authorising",
    )
    parser.add_argument(
        "--submit",
        action="store_true",
        help="if given, submit one share for the first job seen, with a random nonce",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=10.0,
        help="bound, in seconds, on EACH socket read and on each wait phase (default: 10)",
    )
    return parser.parse_args()


class StratumClient(object):
    """A minimal newline-delimited JSON-RPC client over a raw TCP socket."""

    def __init__(self, host, port, timeout):
        self.sock = socket.create_connection((host, port), timeout=timeout)
        self.sock.settimeout(timeout)
        self.timeout = timeout
        self._buf = b""

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass

    def send(self, obj):
        self.sock.sendall((json.dumps(obj) + "\n").encode("utf-8"))

    def read_line(self):
        """Return the next decoded JSON line, or None on a read timeout or EOF.

        Bounded to a single socket-level timeout per call -- callers loop
        with their own overall deadline, so a stalled pool cannot hang
        this script forever.
        """
        while b"\n" not in self._buf:
            try:
                chunk = self.sock.recv(4096)
            except socket.timeout:
                return None
            if not chunk:
                return None
            self._buf += chunk
        line, self._buf = self._buf.split(b"\n", 1)
        line = line.strip()
        if not line:
            return self.read_line()
        try:
            return json.loads(line.decode("utf-8", "replace"))
        except ValueError:
            return None


def main():
    args = parse_args()
    result = {
        "set_difficulty": [],
        "set_difficulty_after_auth": [],
        "diff_after_auth": None,
        "submit_reply": None,
        "suggest_reply_difficulty": None,
        "authorize_result": None,
        "enonce1": None,
        "nonce2_size": None,
        "job_id": None,
        "ntime": None,
    }
    responses = {}
    # Set True right before the mining.authorize request is sent, not after
    # its reply arrives -- ckpool can (and does) push mining.set_difficulty
    # before the authorize response is fully processed, so gating on the
    # reply loses those pushes to a wire-ordering race.
    auth_diff_seen = False

    def pump(client):
        """Read and route exactly one line, if one is available. Returns
        True if a line was consumed, False on timeout/EOF."""
        msg = client.read_line()
        if msg is None:
            return False
        method = msg.get("method")
        if method == "mining.set_difficulty":
            params = msg.get("params") or []
            if params:
                result["set_difficulty"].append(params[0])
                if auth_diff_seen:
                    result["set_difficulty_after_auth"].append(params[0])
        elif method == "mining.notify":
            params = msg.get("params") or []
            if len(params) >= 8:
                result["job_id"] = params[0]
                result["ntime"] = params[7]
        elif "id" in msg and msg.get("id") is not None:
            responses[msg["id"]] = msg
        return True

    def wait_for(client, predicate, deadline):
        while time.time() < deadline:
            if predicate():
                return True
            if not pump(client):
                # No data right now; predicate might still become true from
                # data already buffered by a previous read, so only bail
                # once the deadline itself is spent.
                if time.time() >= deadline:
                    break
        return predicate()

    def drain_for(client, duration):
        """Keep pumping for up to `duration` seconds of wall time, using a
        short per-read timeout so a quiet socket can't stretch this past
        the requested grace window."""
        old_timeout = client.sock.gettimeout()
        client.sock.settimeout(min(old_timeout or duration, 0.2))
        try:
            deadline = time.time() + duration
            while time.time() < deadline:
                pump(client)
        finally:
            client.sock.settimeout(old_timeout)

    try:
        client = StratumClient(args.host, args.port, args.timeout)
    except OSError as exc:
        result["probe_error"] = "connect failed: %s" % exc
        print(json.dumps(result))
        return 1

    try:
        client.send({"id": 1, "method": "mining.subscribe", "params": [args.useragent]})
        deadline = time.time() + args.timeout
        wait_for(client, lambda: 1 in responses, deadline)
        sub = responses.get(1)
        if sub and isinstance(sub.get("result"), list) and len(sub["result"]) >= 3:
            result["enonce1"] = sub["result"][1]
            result["nonce2_size"] = sub["result"][2]

        # Set the marker BEFORE sending mining.authorize, not after its
        # reply arrives: ckpool can (and on a live pool run does) push
        # mining.set_difficulty before this side has finished processing
        # the authorize response, so gating on the reply loses that push
        # to a wire-ordering race.
        auth_diff_seen = True
        client.send({"id": 2, "method": "mining.authorize", "params": [args.user, args.password]})
        deadline = time.time() + args.timeout
        wait_for(client, lambda: 2 in responses, deadline)
        auth = responses.get(2)
        if auth is not None:
            result["authorize_result"] = auth.get("result")

        # Give the pool a bounded window to deliver the post-authorise
        # mining.set_difficulty push(es) and a mining.notify, if one has
        # not arrived yet. Both are unsolicited pushes with no request id
        # to wait on.
        deadline = time.time() + args.timeout
        wait_for(
            client,
            lambda: bool(result["set_difficulty_after_auth"]) and result["job_id"] is not None,
            deadline,
        )
        # Short grace drain: ckpool sometimes follows the first post-auth
        # set_difficulty with a second one moments later -- take the LAST
        # one seen in this window, not the first.
        drain_for(client, 0.5)
        if result["set_difficulty_after_auth"]:
            result["diff_after_auth"] = result["set_difficulty_after_auth"][-1]

        if args.suggest is not None:
            before = len(result["set_difficulty"])
            client.send({"id": 3, "method": "mining.suggest_difficulty", "params": [args.suggest]})
            deadline = time.time() + args.timeout
            wait_for(client, lambda: len(result["set_difficulty"]) > before, deadline)
            if len(result["set_difficulty"]) > before:
                result["suggest_reply_difficulty"] = result["set_difficulty"][-1]

        if args.submit:
            n2size = result["nonce2_size"] or 8
            nonce2 = os.urandom(int(n2size)).hex()
            nonce = os.urandom(4).hex()
            job_id = result["job_id"] or "0"
            ntime = result["ntime"] or "00000000"
            client.send({
                "id": 4,
                "method": "mining.submit",
                "params": [args.user, job_id, nonce2, ntime, nonce],
            })
            deadline = time.time() + args.timeout
            wait_for(client, lambda: 4 in responses, deadline)
            result["submit_reply"] = responses.get(4)
    finally:
        client.close()

    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main())
