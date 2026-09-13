#!/usr/bin/env python3
"""Exercise the real installer preflight against local Stratum wire fixtures."""
import json
from pathlib import Path
import socketserver
import subprocess
import threading
import unittest

SOURCE = Path(__file__).resolve().parents[1] / 'scripts/install-csproxy.sh'
VALID = {'id': 1, 'result': [[['mining.notify', 'session']], '01020304', 8], 'error': None}
NOTIFY = {'id': None, 'method': 'mining.set_difficulty', 'params': [1000000]}


class ProxyPreflightRegression(unittest.TestCase):
    def probe(self, frames):
        requests = []
        payload = b''.join(frame if isinstance(frame, bytes) else json.dumps(frame).encode()+b'\n'
                           for frame in frames)
        class Handler(socketserver.StreamRequestHandler):
            def handle(self):
                requests.append(json.loads(self.rfile.readline()))
                try:
                    self.wfile.write(payload)
                    self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError):
                    pass
        with socketserver.TCPServer(('127.0.0.1', 0), Handler) as server:
            thread = threading.Thread(target=server.handle_request, daemon=True)
            thread.start()
            source = SOURCE.read_text()
            start = source.index('test_pool() {')
            end = source.index('\n}\n', start)+3
            result = subprocess.run(['bash', '-c', source[start:end]+'\ntest_pool "$1" "$2" false',
                                     'fixture', '127.0.0.1', str(server.server_address[1])],
                                    capture_output=True, text=True, timeout=15)
            thread.join(timeout=2)
        self.assertEqual(requests, [{'id': 1, 'method': 'mining.subscribe', 'params': ['csproxy-installer']}])
        return result

    def test_real_pool_difficulty_notification_before_subscription(self):
        result = self.probe([NOTIFY, VALID])
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_correlates_response_after_unrelated_id(self):
        result = self.probe([{**VALID, 'id': 2}, NOTIFY, VALID])
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_rejects_error_or_malformed_subscription(self):
        for frame in ({**VALID, 'error': [20, 'rejected', None]},
                      {**VALID, 'result': 'Initialising'}, {**VALID, 'result': False},
                      {**VALID, 'result': [[], 'not-hex', 8]},
                      {**VALID, 'result': [[], '00', True]},
                      {**VALID, 'result': [[], '00', 8]},
                      {**VALID, 'result': [VALID['result'][0], '00'*16, 8]},
                      {**VALID, 'result': [VALID['result'][0], '00', 9]}, b'not json\n'):
            with self.subTest(frame=frame):
                self.assertNotEqual(self.probe([frame]).returncode, 0)

    def test_wrong_id_or_notifications_alone_never_pass(self):
        for frames in ([NOTIFY], [{**VALID, 'id': 2}], [{**VALID, 'id': True}]):
            with self.subTest(frames=frames):
                self.assertNotEqual(self.probe(frames).returncode, 0)

    def test_bounded_frame_count_and_size(self):
        self.assertNotEqual(self.probe([NOTIFY]*64+[VALID]).returncode, 0)
        self.assertNotEqual(self.probe([b' '*65537+b'\n', VALID]).returncode, 0)


if __name__ == '__main__':
    unittest.main()
