#!/usr/bin/env python3
"""Hosted-runner static regression for difficulty-policy scenarios and stratum_diffprobe."""

import json
import socket
import subprocess
import sys
import threading
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class DiffpolicyRegression(unittest.TestCase):

    def test_main_wires_difficulty_policy_scenarios(self):
        """Assert that main() in regtest-e2e.sh calls the difficulty-policy
        scenarios in the right order, with the pool started before them."""

        script_text = (ROOT / 'testing' / 'regtest-e2e.sh').read_text()

        # Find main() function
        main_start = script_text.find('main() {')
        self.assertNotEqual(main_start, -1, "main() function not found")

        # Find the matching closing brace for main()
        brace_count = 0
        main_end = main_start
        in_main = False
        for i in range(main_start, len(script_text)):
            if script_text[i] == '{':
                brace_count += 1
                in_main = True
            elif script_text[i] == '}' and in_main:
                brace_count -= 1
                if brace_count == 0:
                    main_end = i + 1
                    break

        main_body = script_text[main_start:main_end]

        # Assert that write_diffpolicy_conf is called
        self.assertIn('write_diffpolicy_conf', main_body,
                      "main() must call write_diffpolicy_conf")

        # Assert that start_diffpolicy_pool is called after write_diffpolicy_conf
        diffconf_pos = main_body.find('write_diffpolicy_conf')
        diffpool_pos = main_body.find('start_diffpolicy_pool')
        self.assertNotEqual(diffpool_pos, -1, "main() must call start_diffpolicy_pool")
        self.assertLess(diffconf_pos, diffpool_pos,
                        "start_diffpolicy_pool must be called after write_diffpolicy_conf")

        # Assert that scenario_10 comes before scenario_11
        s10_pos = main_body.find('scenario_10_coinbase_scriptsig')
        self.assertNotEqual(s10_pos, -1, "scenario_10_coinbase_scriptsig not found in main()")

        s11_pos = main_body.find('scenario_11_reject_below_diff')
        self.assertNotEqual(s11_pos, -1, "scenario_11_reject_below_diff not found in main()")
        self.assertLess(s10_pos, s11_pos,
                        "scenario_11 must come after scenario_10")

        # Assert that all four scenarios appear in order
        s12_pos = main_body.find('scenario_12_password_diff_table')
        s13_pos = main_body.find('scenario_13_override_lookup')
        s14_pos = main_body.find('scenario_14_suggest_difficulty')

        self.assertNotEqual(s12_pos, -1, "scenario_12_password_diff_table not found in main()")
        self.assertNotEqual(s13_pos, -1, "scenario_13_override_lookup not found in main()")
        self.assertNotEqual(s14_pos, -1, "scenario_14_suggest_difficulty not found in main()")

        # All four scenarios must be in order
        self.assertLess(s11_pos, s12_pos, "scenario_12 must come after scenario_11")
        self.assertLess(s12_pos, s13_pos, "scenario_13 must come after scenario_12")
        self.assertLess(s13_pos, s14_pos, "scenario_14 must come after scenario_13")

        # All scenarios must come after pool start
        self.assertLess(diffpool_pos, s11_pos,
                        "scenario_11 must come after start_diffpolicy_pool")

    def test_probe_handshake_and_submit(self):
        """Run stratum_diffprobe.py against a fake stratum server and assert
        protocol handling and output."""

        # Server state
        server_ready = threading.Event()
        received_requests = []
        server_error = [None]
        lock = threading.Lock()
        server_socket = [None]

        def read_json_line(conn):
            """Read a newline-delimited JSON line from a socket."""
            buf = b""
            conn.settimeout(2)
            try:
                while b"\n" not in buf:
                    chunk = conn.recv(4096)
                    if not chunk:
                        return None
                    buf += chunk
                line, _ = buf.split(b"\n", 1)
                return json.loads(line.decode())
            except:
                return None

        def handle_client(conn):
            """Handle a single client connection and send canned responses."""
            try:
                # Read subscribe request
                line = read_json_line(conn)
                if line:
                    with lock:
                        received_requests.append(line)
                    # Reply to mining.subscribe
                    reply = {
                        "id": 1,
                        "result": [[["mining.notify", "ab12"]], "deadbeef", 8],
                        "error": None
                    }
                    conn.send((json.dumps(reply) + "\n").encode())
                    # Push mining.set_difficulty(8)
                    push = {"id": None, "method": "mining.set_difficulty", "params": [8]}
                    conn.send((json.dumps(push) + "\n").encode())

                # Read authorize request
                line = read_json_line(conn)
                if line:
                    with lock:
                        received_requests.append(line)
                    # Reply to mining.authorize
                    reply = {
                        "id": 2,
                        "result": True,
                        "error": None
                    }
                    conn.send((json.dumps(reply) + "\n").encode())
                    # Push mining.set_difficulty(64)
                    push = {"id": None, "method": "mining.set_difficulty", "params": [64]}
                    conn.send((json.dumps(push) + "\n").encode())

                # Push mining.notify
                notify = {
                    "id": None,
                    "method": "mining.notify",
                    "params": ["job1", "prevhash", "cb1", "cb2", [], "20000000", "1d00ffff", "5f5e1000", True]
                }
                conn.send((json.dumps(notify) + "\n").encode())

                # Read and process remaining messages
                while True:
                    line = read_json_line(conn)
                    if not line:
                        break

                    with lock:
                        received_requests.append(line)

                    msg_id = line.get("id")
                    method = line.get("method")

                    if msg_id == 3 and method == "mining.suggest_difficulty":
                        # Reply to suggest_difficulty
                        reply = {
                            "id": 3,
                            "result": True,
                            "error": None
                        }
                        conn.send((json.dumps(reply) + "\n").encode())
                        # Push mining.set_difficulty with the suggested value
                        params = line.get("params", [])
                        push_diff = params[0] if params else 2
                        push = {"id": None, "method": "mining.set_difficulty", "params": [push_diff]}
                        conn.send((json.dumps(push) + "\n").encode())

                    elif msg_id == 4 and method == "mining.submit":
                        # Reply to mining.submit with rejection
                        reply = {
                            "id": 4,
                            "result": False,
                            "error": [23, "Above target", None]
                        }
                        conn.send((json.dumps(reply) + "\n").encode())
                        break
            except Exception as e:
                server_error[0] = str(e)
            finally:
                try:
                    conn.close()
                except:
                    pass

        def server_thread_func():
            """Run the fake stratum server."""
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                sock.bind(('127.0.0.1', 0))
                sock.listen(1)
                sock.settimeout(5)

                port = sock.getsockname()[1]
                with lock:
                    received_requests.append(('__port__', port))

                server_socket[0] = sock
                server_ready.set()

                conn, _ = sock.accept()
                handle_client(conn)
            except Exception as e:
                server_error[0] = str(e)
                server_ready.set()
            finally:
                if server_socket[0]:
                    try:
                        server_socket[0].close()
                    except:
                        pass

        # Start server thread
        thread = threading.Thread(target=server_thread_func, daemon=True)
        thread.start()

        # Wait for server to start
        if not server_ready.wait(timeout=5):
            self.fail("Server did not start")

        if server_error[0]:
            self.fail(f"Server error: {server_error[0]}")

        # Get the port
        port = None
        with lock:
            for item in received_requests:
                if isinstance(item, tuple) and item[0] == '__port__':
                    port = item[1]
                    break

        self.assertIsNotNone(port, "Could not get server port")

        # Run the probe
        result = subprocess.run(
            [sys.executable, str(ROOT / 'testing' / 'stratum_diffprobe.py'),
             '--host', '127.0.0.1',
             '--port', str(port),
             '--user', 'alice.w1',
             '--password', 'd=64',
             '--useragent', 'Probe/1',
             '--suggest', '0',
             '--submit',
             '--timeout', '3'],
            capture_output=True,
            text=True
        )

        # Assert exit code
        self.assertEqual(result.returncode, 0, f"Probe failed with stderr: {result.stderr}")

        # Parse the JSON output
        output_json = json.loads(result.stdout)

        # Assert the JSON fields
        self.assertEqual(output_json['diff_after_auth'], 64,
                         "diff_after_auth should be 64 (the set_difficulty AFTER authorize)")

        self.assertEqual(output_json['set_difficulty'], [8, 64, 0],
                         "set_difficulty should be [8, 64, 0]")

        self.assertEqual(output_json['suggest_reply_difficulty'], 0,
                         "suggest_reply_difficulty should be 0")

        self.assertEqual(output_json['enonce1'], "deadbeef",
                         "enonce1 should be 'deadbeef'")

        self.assertEqual(output_json['nonce2_size'], 8,
                         "nonce2_size should be 8")

        self.assertEqual(output_json['job_id'], "job1",
                         "job_id should be 'job1'")

        self.assertEqual(output_json['ntime'], "5f5e1000",
                         "ntime should be '5f5e1000'")

        self.assertIsNotNone(output_json['submit_reply'],
                             "submit_reply should not be null")

        submit_reply = output_json['submit_reply']
        self.assertEqual(submit_reply['result'], False,
                         "submit_reply.result should be false")

        self.assertIn('error', submit_reply,
                      "submit_reply should have 'error' field")

        error = submit_reply['error']
        self.assertEqual(error[1], "Above target",
                         "error message should be 'Above target'")

        # Check server received the right requests
        thread.join(timeout=2)

        with lock:
            reqs = [r for r in received_requests if not isinstance(r, tuple)]

        # Should have subscribe, authorize, suggest, submit requests
        self.assertGreaterEqual(len(reqs), 3, "Server should have received at least 3 requests")

        # Check the subscribe request had the useragent
        subscribe_req = reqs[0]
        self.assertEqual(subscribe_req['id'], 1)
        self.assertEqual(subscribe_req['method'], 'mining.subscribe')
        params = subscribe_req.get('params', [])
        self.assertIn('Probe/1', params)

        # Check submit request parameters
        submit_req = None
        for req in reqs:
            if req.get('method') == 'mining.submit':
                submit_req = req
                break

        self.assertIsNotNone(submit_req, "Should have received mining.submit")
        params = submit_req.get('params', [])
        # params: [user, job_id, nonce2, ntime, nonce]
        self.assertEqual(params[0], 'alice.w1')
        self.assertEqual(params[1], 'job1')
        # params[2] is random nonce2 (hex string)
        self.assertIsInstance(params[2], str)
        self.assertEqual(len(params[2]) % 2, 0)  # hex string has even length
        # params[3] is ntime
        self.assertEqual(params[3], '5f5e1000')
        # params[4] is random nonce (hex string)
        self.assertIsInstance(params[4], str)
        self.assertEqual(len(params[4]) % 2, 0)  # hex string has even length

    def test_probe_reports_connect_failure(self):
        """Run the probe against a closed port and assert it reports the error."""

        # Try to find an unused port by binding and immediately releasing
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            port = s.getsockname()[1]

        # Run the probe against the now-closed port
        result = subprocess.run(
            [sys.executable, str(ROOT / 'testing' / 'stratum_diffprobe.py'),
             '--host', '127.0.0.1',
             '--port', str(port),
             '--user', 'test',
             '--timeout', '1'],
            capture_output=True,
            text=True
        )

        # Assert exit code is 1
        self.assertEqual(result.returncode, 1,
                         f"Probe should exit 1 on connection failure, got {result.returncode}")

        # Parse the JSON output
        output_json = json.loads(result.stdout)

        # Assert probe_error is present
        self.assertIn('probe_error', output_json,
                      "Output should contain 'probe_error' key on connection failure")

        self.assertIsNotNone(output_json['probe_error'],
                             "probe_error value should not be null")


if __name__ == '__main__':
    unittest.main()
