#!/usr/bin/env python3
"""Run monitor.sh over a synthetic log; never read an operator's pool data."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'monitor.sh'
A = 'qzexampleaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
B = 'qzexamplebbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
IDLE = 'qzexampleiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii'


def user(ts, addr, h1, extra=''):
    return (f'[2026-01-01 {ts}.000] User bitcoincash:{addr}:{{"hashrate1m":"{h1}",'
            f'"hashrate5m":"2P","hashrate1hr":"3P","workers":2,"bestshare":1978744860.9{extra}}}')


LOG = '\n'.join([
    '[2026-01-01 00:00:00.000] cashstratum stratifier ready',
    '[2026-01-01 00:00:01.000] Stored local workbase with 12 transactions',
    user('00:00:02', A, '1.4P'),
    user('00:00:02', IDLE, '0'),
    user('00:00:02', B, '0', ',"rejectedcount":7'),
    '[2026-01-01 00:00:02.000] Pool:{"runtime":10,"lastupdate":1,"Users":3,"Workers":4,"Idle":1,"Disconnected":0}',
    '[2026-01-01 00:00:02.000] Pool:{"hashrate1m":"5P","hashrate5m":"4P","hashrate15m":"4P","hashrate1hr":"6P"}',
    '[2026-01-01 00:00:02.000] Pool:{"diff":1.0,"accepted":10,"rejected":100,"SPS1m":2.5}',
    '[2026-01-01 00:00:03.000] Authorised client 1 127.0.0.1 worker bitcoincash:'
    f'{A}.cashstratum-api as user bitcoincash:{A} with diff 20000',
    f'[2026-01-01 00:00:04.000] Dropped client 1 127.0.0.1 user bitcoincash:{A} worker '
    f'bitcoincash:{A}.cashstratum-api',
    "[2026-01-01 00:00:05.000] NiceHash detected from useragent 'NiceHash/1.0.0' for client 2,"
    ' applied difficulty 500000',
    '[2026-01-01 00:00:05.000] Client 2 set difficulty to 10000000 via password parameter',
    f'[2026-01-01 00:00:05.000] Authorised client 2 192.0.2.10 worker {A}.rig1 as user '
    f'bitcoincash:{A} with diff 10000000',
    # rig9 flaps: three sessions of about one second each
    f'[2026-01-01 00:00:10.000] Authorised client 3 192.0.2.20 worker {B}.rig9 as user '
    f'bitcoincash:{B} with diff 1048576',
    f'[2026-01-01 00:00:11.000] Dropped client 3 192.0.2.20 user bitcoincash:{B} worker {B}.rig9',
    f'[2026-01-01 00:00:12.000] Authorised client 4 192.0.2.20 worker {B}.rig9 as user '
    f'bitcoincash:{B} with diff 1048576',
    f'[2026-01-01 00:00:13.000] Dropped client 4 192.0.2.20 user bitcoincash:{B} worker {B}.rig9',
    f'[2026-01-01 00:00:14.000] Authorised client 5 192.0.2.20 worker {B}.rig9 as user '
    f'bitcoincash:{B} with diff 1048576',
    f'[2026-01-01 00:00:15.000] Dropped client 5 192.0.2.20 user bitcoincash:{B} worker {B}.rig9',
    user('00:01:02', A, '1.5P'),
    # the pool skipped its runtime/hashrate lines this minute
    '[2026-01-01 00:01:02.000] Pool:{"diff":1.0,"accepted":20,"rejected":5000100,"SPS1m":2.5}',
    f'[2026-01-01 04:00:05.000] Dropped client 2 192.0.2.10 user bitcoincash:{A} worker {A}.rig1',
    '[2026-01-01 04:00:06.000] ZMQ block hash 00000000000000000001 from endpoint 0',
    '[2026-01-01 04:00:06.000] Block hash changed to 00000000000000000001',
    '[2026-01-01 04:00:07.000] BLOCK ACCEPTED!',
    '[2026-01-01 04:00:08.000] SUBMIT BLOCK RETURNED: inconclusive',
    '[2026-01-01 04:00:09.000] Generator returned failure in update_base, retry #1',
]) + '\n'


class MonitorRegression(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='cashstratum-monitor-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'logs').mkdir()
        (self.root / 'logs' / 'cashstratum.log').write_text(LOG)

    def run_monitor(self, *args, color=False, env=None):
        values = dict(os.environ, CASHSTRATUM_DIR=str(self.root), CS_MONITOR_FOLLOW='0')
        values.pop('CS_MONITOR_GROUPS', None)
        values.pop('NO_COLOR', None)
        if not color:
            values['NO_COLOR'] = '1'
        values.update(env or {})
        result = subprocess.run(['bash', str(SCRIPT), *args], env=values, text=True,
                                capture_output=True, stdin=subprocess.DEVNULL, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout

    def test_active_user_is_one_compact_row_without_prefix(self):
        out = self.run_monitor()
        rows = [l for l in out.splitlines() if l.startswith('00:00:02 ' + A)]
        self.assertEqual(len(rows), 1, out)
        self.assertIn('1.4P', rows[0])
        self.assertIn('2w', rows[0])
        self.assertIn('best 1.98G', rows[0])
        self.assertNotIn('bitcoincash:', out)

    def test_idle_user_hidden_but_user_with_rejects_kept(self):
        out = self.run_monitor()
        self.assertNotIn(IDLE, out)
        self.assertIn('rej 7', out)

    def test_pool_lines_fold_into_one_row(self):
        out = self.run_monitor()
        pool = [l for l in out.splitlines() if ' POOL ' in l]
        self.assertEqual(len(pool), 2, out)
        self.assertIn('5P / 4P / 6P', pool[0])
        self.assertIn('users 3', pool[0])
        self.assertIn('rejected +5.00M', pool[1])
        # a minute without hashrate/runtime lines must not repeat stale values
        self.assertIn('(hashrate not logged)', pool[1])
        self.assertNotIn('users 3', pool[1])
        self.assertNotIn('Pool:{', out)

    def test_api_probe_hidden(self):
        out = self.run_monitor()
        self.assertNotIn('cashstratum-api', out)
        self.assertNotIn('127.0.0.1', out)

    def test_connect_tagged_with_difficulty_source_and_drop_has_duration(self):
        out = self.run_monitor()
        self.assertIn(f'+ {A}.rig1  192.0.2.10  d=10000000  [NiceHash floor 500000 d=10000000 (password)]',
                      out)
        self.assertIn(f'- {A}.rig1  192.0.2.10  (4h00m)', out)

    def test_connect_green_and_drop_red_with_colour(self):
        out = self.run_monitor(color=True)
        plus = [l for l in out.splitlines() if ' + ' in l and 'rig1' in l]
        minus = [l for l in out.splitlines() if ' - ' in l and 'rig1' in l]
        self.assertTrue(plus, out)
        self.assertTrue(minus, out)
        self.assertIn('\x1b[1;32m', plus[0])
        self.assertIn('\x1b[31m', minus[0])

    def test_flapping_worker_folds_into_flap_line(self):
        out = self.run_monitor()
        flap = [l for l in out.splitlines() if 'FLAP' in l]
        self.assertEqual(len(flap), 1, out)
        self.assertIn(f'{B}.rig9', flap[0])
        self.assertIn('2 quick drops', flap[0])
        # the third connect is suppressed while the worker is flapping
        self.assertEqual(sum(1 for l in out.splitlines() if l.startswith('00:00:14 +')), 0, out)

    def test_flap_uses_logged_age_when_present(self):
        log = self.root / 'logs' / 'cashstratum.log'
        log.write_text(
            f'[2026-01-01 00:00:11.000] Dropped client 3 192.0.2.20 user bitcoincash:{B} worker '
            f'{B}.rig9 reason=client_eof age=1.2\n'
            f'[2026-01-01 00:00:13.000] Dropped client 4 192.0.2.20 user bitcoincash:{B} worker '
            f'{B}.rig9 reason=client_eof age=0.8\n')
        out = self.run_monitor()
        self.assertIn('2 quick drops (last session 0s, client_eof)', out)

    def test_blocks_and_errors_kept(self):
        out = self.run_monitor()
        self.assertIn('* BLOCK ACCEPTED!', out)
        self.assertIn('NEW NETWORK BLOCK ━━━━ 00000000000000000001', out)
        self.assertIn('SUBMIT BLOCK RETURNED', out)
        self.assertIn('Generator returned failure', out)
        self.assertIn('Stored local workbase with 12 transactions', out)

    def test_all_mode_passes_every_line(self):
        out = self.run_monitor('--all')
        self.assertEqual(out, LOG)

    def test_groups_file_labels_address(self):
        groups = self.root / 'groups.txt'
        groups.write_text(f'# ours\nbitcoincash:{A} ours green\n')
        out = self.run_monitor('--groups', str(groups))
        self.assertIn(f'{A} [ours]', out)

    def test_groups_file_autoloads_from_install_dir(self):
        (self.root / 'monitor-groups.txt').write_text(f'{A} ours\n')
        self.assertIn(f'{A} [ours]', self.run_monitor())

    def test_colour_only_without_no_color(self):
        self.assertNotIn('\x1b[', self.run_monitor())
        self.assertIn('\x1b[', self.run_monitor(color=True))

    def test_help_and_bad_option(self):
        values = dict(os.environ, CASHSTRATUM_DIR=str(self.root))
        ok = subprocess.run(['bash', str(SCRIPT), '--help'], env=values, text=True,
                            capture_output=True, timeout=30)
        self.assertEqual(ok.returncode, 0)
        self.assertIn('--all', ok.stdout)
        bad = subprocess.run(['bash', str(SCRIPT), '--nope'], env=values, text=True,
                             capture_output=True, timeout=30)
        self.assertEqual(bad.returncode, 2)


if __name__ == '__main__':
    unittest.main()
