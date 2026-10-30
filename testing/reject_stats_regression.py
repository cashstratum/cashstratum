#!/usr/bin/env python3
"""reject_stats_regression.py -- per-worker reject counters and disconnect
reasons, observed against a live regtest pool.

Drives a stratum V1 pool with raw sockets (no hashing: a random nonce is
always below any assigned difficulty >= 1) and then reads what the pool
publishes:

  * <logdir>/users/<address>  -- JSON rewritten by the stats cycle
  * the main pool log         -- "Dropped client ..." lines

Checks:
  reject counters   low-diff submits bump rejectedcount, rejects.lowdiff,
                    rejected (credited difficulty) and lastrejectreason;
                    one submit each of bad ntime / unknown job / malformed
                    params / wrong workername bumps its own bucket; a replayed
                    low-diff share is a second lowdiff; every reject key and
                    all buckets are present, and a never-hit bucket stays 0.
  disconnect info   a client that submits one share and closes cleanly is
                    logged with reason=peer_eof and counted in disconnects /
                    lastdisconnectreason; invalid JSON is logged with
                    reason=bad_json; three connect/hang-up cycles raise
                    connects and disconnects by 3.

The pool only writes a users file for a user that has an accepted share or a
reject, so every session that must show up there submits one low-diff share.

The users file is refreshed on a ~30-60 s cycle, so every file check polls
with a bounded timeout. Stdlib only. Exit 0 when every check passed, 1
otherwise; each check prints one PASS/FAIL line.
"""

import argparse
import json
import os
import re
import socket
import sys
import time

BUCKETS = ("stale", "duplicate", "lowdiff", "ntime", "badjob", "malformed", "worker")
REJECT_KEYS = ("rejected", "rejectedcount", "rejects", "lastreject", "lastrejectreason",
               "rejectrate1m", "rejectrate5m", "rejectrate1hr")

failures = 0
checks = 0


def check(desc, ok):
    """Record and print one PASS/FAIL line."""
    global failures, checks
    checks += 1
    if not ok:
        failures += 1
    print("%s: %s" % ("PASS" if ok else "FAIL", desc), flush=True)


class Stratum(object):
    """Minimal newline-delimited JSON-RPC client."""

    def __init__(self, host, port, timeout):
        self.sock = socket.create_connection((host, port), timeout=timeout)
        self.sock.settimeout(timeout)
        self.buf = b""
        self.next_id = 1
        self.replies = {}
        self.diff = None
        self.job = None
        self.nonce2_size = 8

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass

    def send_raw(self, data):
        self.sock.sendall(data)

    def request(self, method, params):
        rid = self.next_id
        self.next_id += 1
        self.send_raw((json.dumps({"id": rid, "method": method, "params": params}) + "\n").encode())
        return rid

    def _line(self):
        while b"\n" not in self.buf:
            try:
                chunk = self.sock.recv(4096)
            except socket.timeout:
                return None
            if not chunk:
                return None
            self.buf += chunk
        line, self.buf = self.buf.split(b"\n", 1)
        return line.strip()

    def pump(self):
        """Consume one line and route it; False on timeout or EOF."""
        line = self._line()
        if line is None:
            return False
        if not line:
            return True
        try:
            msg = json.loads(line.decode("utf-8", "replace"))
        except ValueError:
            return True
        method = msg.get("method")
        params = msg.get("params") or []
        if method == "mining.set_difficulty" and params:
            self.diff = params[0]
        elif method == "mining.notify" and len(params) >= 8:
            self.job = (params[0], params[7])
        elif msg.get("id") is not None:
            self.replies[msg["id"]] = msg
        return True

    def wait_reply(self, rid, timeout):
        deadline = time.time() + timeout
        while rid not in self.replies and time.time() < deadline:
            if not self.pump():
                break
        return self.replies.get(rid)

    def wait_job(self, timeout):
        deadline = time.time() + timeout
        while (self.job is None or self.diff is None) and time.time() < deadline:
            if not self.pump():
                break
        return self.job is not None


def open_session(host, port, user, timeout=10.0):
    """Subscribe and authorise; return the client once a job and difficulty arrived."""
    cli = Stratum(host, port, timeout)
    rid = cli.request("mining.subscribe", ["reject-stats-probe"])
    sub = cli.wait_reply(rid, timeout)
    if sub and isinstance(sub.get("result"), list) and len(sub["result"]) >= 3:
        cli.nonce2_size = int(sub["result"][2])
    rid = cli.request("mining.authorize", [user, "x"])
    auth = cli.wait_reply(rid, timeout)
    cli.authorised = bool(auth and auth.get("result") is True)
    cli.wait_job(timeout)
    return cli


def reject_text(reply):
    """The human reason from a reject reply, or '' when there is none."""
    err = (reply or {}).get("error")
    if isinstance(err, list) and len(err) > 1:
        return str(err[1])
    return str(err or "")


def submit(cli, user, job_id, ntime, nonce=None, nonce2=None, params=None):
    """Send one mining.submit and return the reply. Random nonces unless pinned."""
    if params is None:
        params = [user, job_id, nonce2 or os.urandom(cli.nonce2_size).hex(), ntime,
                  nonce or os.urandom(4).hex()]
    return cli.wait_reply(cli.request("mining.submit", params), 10)


def read_users_file(logdir, addr):
    try:
        with open(os.path.join(logdir, "users", addr)) as fp:
            return json.load(fp)
    except (OSError, ValueError):
        return None


def find_worker(doc, addr, name):
    """The worker[] entry whose workername is `name` (bare or address-prefixed)."""
    for w in (doc or {}).get("worker", []) or []:
        wn = str(w.get("workername", ""))
        if wn == name or wn == "%s.%s" % (addr, name) or wn.endswith("." + name):
            return w
    return None


def poll(logdir, addr, predicate, timeout):
    """Re-read the users file until predicate(doc) holds; return the last doc."""
    deadline = time.time() + timeout
    doc = read_users_file(logdir, addr)
    while time.time() < deadline:
        try:
            if doc is not None and predicate(doc):
                return doc
        except (KeyError, TypeError, ValueError):
            pass
        time.sleep(2)
        doc = read_users_file(logdir, addr)
    return doc


def num(d, key):
    try:
        return int((d or {}).get(key, 0) or 0)
    except (TypeError, ValueError):
        return 0


def bucket(d, name):
    return num((d or {}).get("rejects") or {}, name)


def baseline(logdir, addr, name):
    """(user, worker) counters before the probe acts; empty dicts if the file is new."""
    doc = read_users_file(logdir, addr)
    return doc or {}, find_worker(doc, addr, name) or {}


def log_has(log_path, worker, reason, since):
    """True if a 'Dropped client' line for `worker` carries ` reason=<reason>`."""
    try:
        with open(log_path, errors="replace") as fp:
            fp.seek(since)
            text = fp.read()
    except OSError:
        return False
    pat = re.compile(r"Dropped client .*worker %s\b.* reason=%s\b" % (re.escape(worker), reason))
    return any(pat.search(line) for line in text.splitlines())


def log_size(log_path):
    try:
        return os.path.getsize(log_path)
    except OSError:
        return 0


def wait_log(log_path, worker, reason, since, timeout=15):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if log_has(log_path, worker, reason, since):
            return True
        time.sleep(1)
    return False


def test_rejects(args):
    addr = args.reject_addr
    name = "rej"
    user = "%s.%s" % (addr, name)
    n_low = 5

    ub, wb = baseline(args.logdir, addr, name)
    cli = open_session(args.host, args.port, user)
    check("reject worker authorised and received a job", cli.authorised and cli.job is not None)
    if cli.job is None:
        cli.close()
        return
    job_id, ntime = cli.job
    diff = int(cli.diff or 0)

    # B1: low-diff submits with distinct nonces.
    for _ in range(n_low):
        reply = submit(cli, user, job_id, ntime)
        if reject_text(reply) != "Above target":
            check("lowdiff submit answered 'Above target' (got %r)" % reject_text(reply), False)
            break

    def b1_done(doc):
        w = find_worker(doc, addr, name)
        return num(w, "rejectedcount") >= num(wb, "rejectedcount") + n_low and \
            num(doc, "rejectedcount") >= num(ub, "rejectedcount") + n_low

    doc = poll(args.logdir, addr, b1_done, args.poll_timeout)
    w = find_worker(doc, addr, name) or {}
    for label, ent, base in (("worker", w, wb), ("user", doc or {}, ub)):
        check("B1 %s rejectedcount +%d (%d -> %d)" % (
            label, n_low, num(base, "rejectedcount"), num(ent, "rejectedcount")),
            num(ent, "rejectedcount") - num(base, "rejectedcount") == n_low)
        check("B1 %s rejects.lowdiff +%d" % (label, n_low),
              bucket(ent, "lowdiff") - bucket(base, "lowdiff") == n_low)
        check("B1 %s rejected += %d x diff %d (%d -> %d)" % (
            label, n_low, diff, num(base, "rejected"), num(ent, "rejected")),
            diff > 0 and num(ent, "rejected") - num(base, "rejected") == n_low * diff)
        check("B1 %s lastrejectreason == 'Above target' (got %r)" % (
            label, ent.get("lastrejectreason")), ent.get("lastrejectreason") == "Above target")
        check("B1 %s lastreject is recent" % label,
              abs(time.time() - num(ent, "lastreject")) < 180)

    # B2: one of each remaining kind. The same pinned share is sent twice. The
    # pool runs its duplicate check only for a share that meets the target, which
    # this probe cannot produce cheaply, so the replay is answered 'Above target'
    # again and counts as a second lowdiff.
    ub, wb = (doc or {}), w
    pinned_nonce, pinned_nonce2 = os.urandom(4).hex(), os.urandom(cli.nonce2_size).hex()
    r_first = submit(cli, user, job_id, ntime, pinned_nonce, pinned_nonce2)
    r_dup = submit(cli, user, job_id, ntime, pinned_nonce, pinned_nonce2)
    r_ntime = submit(cli, user, job_id, "00000000")
    r_job = submit(cli, user, "deadbeef", ntime)
    r_mal = submit(cli, user, job_id, ntime, params=[user, job_id, "00"])
    r_wrk = submit(cli, user, job_id, ntime, params=[
        addr + ".other", job_id, os.urandom(cli.nonce2_size).hex(), ntime, os.urandom(4).hex()])
    check("B2 pinned share first answered 'Above target' (got %r)" % reject_text(r_first),
          reject_text(r_first) == "Above target")
    check("B2 replayed low-diff share answered 'Above target' (got %r)" % reject_text(r_dup),
          reject_text(r_dup) == "Above target")
    check("B2 bad ntime answered 'Ntime out of range' (got %r)" % reject_text(r_ntime),
          reject_text(r_ntime) == "Ntime out of range")
    check("B2 unknown job answered 'Invalid JobID' (got %r)" % reject_text(r_job),
          reject_text(r_job) == "Invalid JobID")
    check("B2 short params answered 'Invalid array size' (got %r)" % reject_text(r_mal),
          reject_text(r_mal) == "Invalid array size")
    check("B2 wrong workername answered 'Worker mismatch' (got %r)" % reject_text(r_wrk),
          reject_text(r_wrk) == "Worker mismatch")
    cli.close()

    expect = {"lowdiff": 2, "duplicate": 0, "ntime": 1, "badjob": 1, "malformed": 1, "worker": 1}
    total = sum(expect.values())

    def b2_done(d):
        ent = find_worker(d, addr, name)
        return num(ent, "rejectedcount") >= num(wb, "rejectedcount") + total and \
            num(d, "rejectedcount") >= num(ub, "rejectedcount") + total

    doc = poll(args.logdir, addr, b2_done, args.poll_timeout)
    w = find_worker(doc, addr, name) or {}
    for label, ent, base in (("worker", w, wb), ("user", doc or {}, ub)):
        check("B2 %s rejectedcount +%d (%d -> %d)" % (
            label, total, num(base, "rejectedcount"), num(ent, "rejectedcount")),
            num(ent, "rejectedcount") - num(base, "rejectedcount") == total)
        for key, want in sorted(expect.items()):
            got = bucket(ent, key) - bucket(base, key)
            check("B2 %s rejects.%s +%d (got +%d)" % (label, key, want, got), got == want)
        check("B2 %s rejects.stale unchanged" % label,
              bucket(ent, "stale") == bucket(base, "stale"))

    # B5: the reject user and worker expose every reject key and every bucket,
    # and the bucket this probe never hits is still present at 0.
    for label, ent in (("worker", w), ("user", doc or {})):
        missing = [k for k in REJECT_KEYS if k not in ent]
        check("B5 %s carries every reject key (missing: %s)" % (label, missing or "none"),
              not missing)
        rj = ent.get("rejects") or {}
        check("B5 %s has all %d buckets present" % (label, len(BUCKETS)),
              all(b in rj for b in BUCKETS))
        check("B5 %s never-hit bucket 'stale' is 0" % label,
              "stale" in rj and num(rj, "stale") == 0)


def hang_up(cli, user, graceful=False):
    """Submit one low-diff share, then close.

    The share gets the user stored (the pool skips users with neither an
    accepted share nor a reject). graceful=True half-closes first so the pool
    reads a clean EOF rather than a reset.
    """
    if cli.job is not None:
        submit(cli, user, cli.job[0], cli.job[1])
    if graceful:
        try:
            cli.sock.shutdown(socket.SHUT_WR)
        except OSError:
            pass
    cli.close()


def test_disconnects(args):
    addr = args.conn_addr
    log_path = args.log

    # C1: authorise, submit one share, then close cleanly.
    _, c1_base = baseline(args.logdir, addr, "c1")
    since = log_size(log_path)
    cli = open_session(args.host, args.port, addr + ".c1")
    check("C1 worker authorised before hang-up", cli.authorised)
    hang_up(cli, addr + ".c1", graceful=True)
    check("C1 main log has 'Dropped client ... reason=peer_eof'",
          wait_log(log_path, "%s.c1" % addr, "peer_eof", since))
    check("C1 'Dropped client' line carries age=",
          re.search(r"worker %s\.c1\b.* age=\d+" % re.escape(addr),
                    open(log_path, errors="replace").read()[since:]) is not None
          if os.path.exists(log_path) else False)

    # C2: invalid JSON.
    since = log_size(log_path)
    cli = open_session(args.host, args.port, addr + ".c2")
    check("C2 worker authorised before bad JSON", cli.authorised)
    cli.send_raw(b"this is not json\n")
    cli.pump()
    cli.close()
    check("C2 main log has 'Dropped client ... reason=bad_json'",
          wait_log(log_path, "%s.c2" % addr, "bad_json", since))

    # C4: repeated authorise / hang-up cycles on one worker.
    cycles = 3
    _, w0 = baseline(args.logdir, addr, "c4")
    for _ in range(cycles):
        cli = open_session(args.host, args.port, addr + ".c4")
        hang_up(cli, addr + ".c4")
        time.sleep(1)

    def c4_done(d):
        w = find_worker(d, addr, "c4")
        return num(w, "connects") >= num(w0, "connects") + cycles and \
            num(w, "disconnects") >= num(w0, "disconnects") + cycles

    doc = poll(args.logdir, addr, c4_done, args.poll_timeout)
    w = find_worker(doc, addr, "c4") or {}
    check("C4 connects +%d (%d -> %d)" % (cycles, num(w0, "connects"), num(w, "connects")),
          num(w, "connects") - num(w0, "connects") == cycles)
    check("C4 disconnects +%d (%d -> %d)" % (cycles, num(w0, "disconnects"),
                                            num(w, "disconnects")),
          num(w, "disconnects") - num(w0, "disconnects") == cycles)
    check("C4 lastdisconnectreason in {peer_eof, peer_reset} (got %r)" % w.get(
        "lastdisconnectreason"), w.get("lastdisconnectreason") in ("peer_eof", "peer_reset"))

    # C1 (worker side): last disconnect reason for a plain hang-up.
    want = num(c1_base, "disconnects") + 1
    doc = poll(args.logdir, addr, lambda d: find_worker(d, addr, "c1") is not None and
               num(find_worker(d, addr, "c1"), "disconnects") >= want, args.poll_timeout)
    w = find_worker(doc, addr, "c1") or {}
    check("C1 worker disconnects +1 (%d -> %d)" % (num(c1_base, "disconnects"),
                                                  num(w, "disconnects")),
          num(w, "disconnects") == want)
    check("C1 worker lastdisconnectreason == 'peer_eof' (got %r)" % w.get("lastdisconnectreason"),
          w.get("lastdisconnectreason") == "peer_eof")


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", required=True, type=int, help="pool stratum port")
    ap.add_argument("--logdir", required=True, help="pool logdir (holds users/)")
    ap.add_argument("--log", required=True, help="pool main log file")
    ap.add_argument("--reject-addr", required=True, help="address for the reject worker")
    ap.add_argument("--conn-addr", required=True, help="address for the connect/disconnect workers")
    ap.add_argument("--poll-timeout", type=float, default=90.0,
                    help="seconds to wait for each users-file refresh (default 90)")
    args = ap.parse_args()

    for fn in (test_rejects, test_disconnects):
        try:
            fn(args)
        except (OSError, ValueError) as exc:
            check("%s completed (%s)" % (fn.__name__, exc), False)

    print("%d/%d reject-stats checks passed" % (checks - failures, checks), flush=True)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
