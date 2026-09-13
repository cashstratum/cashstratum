#!/usr/bin/env python3
"""Prove cleanhost infrastructure failures fail closed without running Docker."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HARNESS = Path(__file__).with_name('cleanhost-installer-test.sh')


class CleanhostRegression(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='cashstratum-cleanhost-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'testing').mkdir()
        shutil.copy2(HARNESS, self.root / 'testing' / HARNESS.name)
        (self.root / 'README.md').write_text('Checkout fixture\n')
        self.commands = self.root / 'commands'
        self.commands.mkdir()
        self.docker = self.commands / 'docker'
        for args in (['init', '-q'], ['config', 'core.hooksPath', '/dev/null'],
                     ['config', 'user.name', 'Test'], ['config', 'user.email', 'test@example.invalid'],
                     ['config', 'commit.gpgsign', 'false'], ['add', 'README.md', 'testing'],
                     ['update-index', '--add', '--cacheinfo', '160000,'+'a'*40+',src/secp256k1'],
                     ['commit', '-qm', 'Fixture']):
            subprocess.run(['git', '-C', str(self.root), *args], check=True, capture_output=True)

    def run_harness(self, script):
        self.docker.write_text('#!/bin/sh\n' + script)
        self.docker.chmod(0o755)
        environment = {**os.environ, 'PATH': str(self.commands)+os.pathsep+os.environ['PATH'],
                       'CLEANHOST_LOG_DIR': str(self.root/'logs')}
        return subprocess.run(['bash', str(self.root/'testing'/HARNESS.name), 'ubuntu:24.04'],
                              env=environment, capture_output=True, text=True)

    def test_container_failure_is_not_hidden_by_tee(self):
        result = self.run_harness('if [ "$1" = info ]; then exit 0; fi\necho fixture-build-failed >&2\nexit 73\n')
        self.assertEqual(result.returncode, 73, result.stdout + result.stderr)
        self.assertNotIn('All requested distributions passed', result.stdout)
        self.assertIn('fixture-build-failed', (self.root/'logs/ubuntu-24.04.log').read_text())
        self.assertTrue((self.root/'logs/CHECKOUT-SHA256.txt').is_file())

    def test_simulated_service_lifecycle_matches_installer_checks(self):
        # Execute the actual embedded stub, rather than a second implementation.
        code = HARNESS.read_text().split("<<'SYSTEMCTL'\n", 1)[1].split("\nSYSTEMCTL", 1)[0]
        executable = self.commands / 'systemctl'
        executable.write_text(code + '\n')
        executable.chmod(0o755)
        environment = {**os.environ, 'SYSTEMCTL_FIXTURE_DIR': str(self.root/'service-state'),
                       'SYSTEMCTL_FIXTURE_LOG': str(self.root/'service-calls.log')}
        def call(*arguments):
            return subprocess.run([str(executable), *arguments], env=environment,
                                  capture_output=True, text=True)
        self.assertNotEqual(call('is-active', '--quiet', 'csproxy').returncode, 0)
        self.assertNotEqual(call('is-enabled', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('daemon-reload').returncode, 0)
        self.assertEqual(call('start', 'csproxy').returncode, 0)
        self.assertEqual(call('is-active', '--quiet', 'csproxy.service').returncode, 0)
        self.assertNotEqual(call('is-enabled', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('enable', 'csproxy').returncode, 0)
        self.assertEqual(call('stop', 'csproxy').returncode, 0)
        self.assertNotEqual(call('is-active', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('is-enabled', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('restart', 'csproxy').returncode, 0)
        self.assertEqual(call('is-active', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('disable', 'csproxy').returncode, 0)
        self.assertNotEqual(call('is-enabled', '--quiet', 'csproxy').returncode, 0)
        self.assertEqual(call('is-active', '--quiet', 'csproxy').returncode, 0)
        self.assertNotEqual(call('is-active', '--quiet', 'another-service').returncode, 0)
        self.assertEqual(call('unsupported', 'csproxy').returncode, 2)
        self.assertIn('start csproxy', (self.root/'service-calls.log').read_text())

    def test_unavailable_daemon_stops_before_claiming_tests_ran(self):
        result = self.run_harness('exit 73\n')
        self.assertEqual(result.returncode, 2)
        self.assertIn('no checks ran', result.stderr)
        self.assertFalse((self.root/'logs/CHECKOUT-SHA256.txt').exists())


if __name__ == '__main__':
    unittest.main()
