#!/usr/bin/env python3
"""Run retention against disposable trees; never use an operator's pool data."""
import gzip
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'clean-old-blocks.sh'
MARKER = '# cashstratum solve index v1: verified baseline\n'


class RetentionRegression(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='cashstratum-retention-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.logs = self.root / 'logs'
        self.logs.mkdir()
        self.index = self.root / 'solved-heights.idx'
        self.seed = self.root / 'verified.idx'
        self.seed.write_text('')
        self.log = self.logs / 'cashstratum.log'
        self.log.write_text('Pool started\n')

    def run_cleanup(self, *args, env=None, **kwargs):
        values = dict(os.environ, CASHSTRATUM_DIR=str(self.root))
        values.pop('SOLVE_INDEX', None)
        values.update(env or {})
        return subprocess.run(['bash', str(SCRIPT), *args], env=values,
                              text=True, capture_output=True, **kwargs)

    def aged_height(self, height):
        path = self.logs / f'{height:08x}'
        path.mkdir()
        old = time.time() - 100 * 86400
        os.utime(path, (old, old))
        return path

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def assert_failure(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def initialize(self):
        result = self.run_cleanup('--seed-index', str(self.seed))
        self.assert_success(result)

    def test_missing_or_empty_index_never_deletes(self):
        solved = self.aged_height(123)
        for empty in (False, True):
            if empty:
                self.index.write_text('')
            self.assert_failure(self.run_cleanup())
            self.assertTrue(solved.exists())
            self.assertEqual(self.index.exists(), empty)

    def test_seed_and_dual_name_gzip_preserve_all_heights(self):
        paths = {h: self.aged_height(h) for h in (123, 456, 789, 900)}
        self.seed.write_text('000001c8\n')  # historical solve no longer in logs
        self.log.write_text('Solved and confirmed block 789\n')
        with gzip.open(self.logs / 'ckpool.log.1.gz', 'wt') as stream:
            stream.write('Solved and confirmed block 123\n')
        self.initialize()
        self.assertTrue(all(path.exists() for path in paths.values()))
        self.assert_success(self.run_cleanup())
        self.assertTrue(all(paths[h].exists() for h in (123, 456, 789)))
        self.assertFalse(paths[900].exists())
        self.assertEqual(self.index.read_text(), MARKER + '0000007b\n000001c8\n00000315\n')

    def test_legacy_nonempty_index_requires_verified_import(self):
        path = self.aged_height(456)
        self.index.write_text('000001c8\n')
        self.assert_failure(self.run_cleanup())
        self.assertTrue(path.exists())
        self.assertEqual(self.index.read_text(), '000001c8\n')
        self.assert_success(self.run_cleanup('--seed-index', str(self.index)))
        self.assert_success(self.run_cleanup())
        self.assertTrue(path.exists())
        self.assertIn('000001c8\n', self.index.read_text())

    def test_explicit_empty_seed_allows_verified_no_solve_pool(self):
        path = self.aged_height(123)
        self.initialize()
        self.assertEqual(self.index.read_text(), MARKER)
        self.assertTrue(path.exists())
        self.assert_success(self.run_cleanup())
        self.assertFalse(path.exists())

    def test_corrupt_gzip_refuses_without_changing_index(self):
        self.initialize()
        before = self.index.read_bytes()
        path = self.aged_height(123)
        (self.logs / 'cashstratum.log.1.gz').write_bytes(b'not gzip')
        self.assert_failure(self.run_cleanup())
        self.assertEqual(self.index.read_bytes(), before)
        self.assertTrue(path.exists())

    def test_missing_logs_refuses_even_with_index(self):
        self.initialize()
        path = self.aged_height(123)
        self.log.unlink()
        self.assert_failure(self.run_cleanup())
        self.assertTrue(path.exists())

    def test_invalid_seed_and_existing_index_are_not_replaced(self):
        path = self.aged_height(123)
        self.seed.write_text('123\n')
        self.assert_failure(self.run_cleanup('--seed-index', str(self.seed)))
        self.assertFalse(self.index.exists())
        self.index.write_text('garbage\n')
        self.assert_failure(self.run_cleanup())
        self.assertEqual(self.index.read_text(), 'garbage\n')
        self.assertTrue(path.exists())

    def test_dry_run_changes_neither_index_nor_logs(self):
        self.initialize()
        path = self.aged_height(123)
        self.log.write_text('Solved and confirmed block 456\n')
        before = {p: p.read_bytes() for p in self.root.rglob('*') if p.is_file()}
        result = self.run_cleanup('--dry-run')
        self.assert_success(result)
        self.assertIn('WOULD REMOVE:', result.stdout)
        self.assertTrue(path.exists())
        after = {p: p.read_bytes() for p in self.root.rglob('*') if p.is_file()}
        self.assertEqual(before, after)

    def test_dry_run_seed_does_not_create_index(self):
        self.assert_success(self.run_cleanup('--dry-run', '--seed-index', str(self.seed)))
        self.assertFalse(self.index.exists())

    def test_lock_refuses_concurrent_run(self):
        self.initialize()
        lock = self.root / 'solved-heights.idx.lock'
        lock.mkdir()
        self.assert_failure(self.run_cleanup())
        self.assertTrue(lock.exists())

    def test_unreadable_log_refuses_without_losing_protection(self):
        self.initialize()
        path = self.aged_height(123)
        before = self.index.read_bytes()
        self.log.chmod(0)
        self.addCleanup(self.log.chmod, 0o644)
        options = {}
        if os.geteuid() == 0:
            # Exercise real permission failure even in root-run Linux CI.
            self.root.chmod(0o777)
            self.logs.chmod(0o777)
            options.update(user=65534, group=65534)
        self.assert_failure(self.run_cleanup(**options))
        self.assertTrue(path.exists())
        self.assertEqual(self.index.read_bytes(), before)

    def test_index_read_error_refuses(self):
        self.initialize()
        before = self.index.read_bytes()
        path = self.aged_height(123)
        commands = self.root / 'commands'
        commands.mkdir()
        cat = commands / 'cat'
        cat.write_text('#!/bin/bash\nfor arg in "$@"; do\n'
                       ' case "$arg" in */solved-heights.idx) exit 1 ;; esac\n'
                       'done\nexec /bin/cat "$@"\n')
        cat.chmod(0o755)
        self.assert_failure(self.run_cleanup(env={'PATH': str(commands) + ':' + os.environ['PATH']}))
        self.assertTrue(path.exists())
        self.assertEqual(self.index.read_bytes(), before)

    def test_symlink_log_refuses(self):
        self.initialize()
        target = self.root / 'external.log'
        target.write_text('Solved and confirmed block 123\n')
        (self.logs / 'ckpool.log.1').symlink_to(target)
        self.assert_failure(self.run_cleanup())
        self.assertTrue(target.exists())

    def test_rotated_gzip_is_indexed_before_removal(self):
        self.initialize()
        path = self.aged_height(123)
        rotated = self.logs / 'cashstratum.log.1.gz'
        with gzip.open(rotated, 'wt') as stream:
            stream.write('Solved and confirmed block 123\n')
        old = time.time() - 100 * 86400
        os.utime(rotated, (old, old))
        self.assert_success(self.run_cleanup())
        self.assertTrue(path.exists())
        self.assertFalse(rotated.exists())
        self.assertIn('0000007b\n', self.index.read_text())


if __name__ == '__main__':
    unittest.main()
