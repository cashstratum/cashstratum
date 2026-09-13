#!/usr/bin/env python3
"""Non-root installer fixtures: real scripts, local RPC, disposable source/artifacts."""
import base64
import http.server
import json
import os
import pathlib
import pwd
import subprocess
import tempfile
import threading
import unittest

REPO = pathlib.Path(__file__).resolve().parents[1]
ADDRESS = 'bchreg:qpm2qsznhks23z7629mms6s4cwef74vcwvgpseywxy'

class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='cashstratum-installer-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name).resolve()/'with spaces'
        self.root.mkdir()
        self.source = self.root / 'source'
        (self.source / 'src').mkdir(parents=True)
        for name in ('autogen.sh', 'configure', 'src/ckpool', 'src/ckpmsg', 'src/notifier'):
            p = self.source / name
            p.write_text('#!/bin/sh\nexit 0\n')
            p.chmod(0o755)
        (self.source / 'Makefile').write_text('all:\n\t@true\n')
        self.commands = self.root / 'commands'
        self.commands.mkdir()
        for name in ('autoconf', 'automake', 'pkg-config'):
            executable = self.commands/name
            executable.write_text('#!/bin/sh\nexit 0\n'); executable.chmod(0o755)
        self.stage = self.root / 'stage'
        self.stage.mkdir()
        self.node_brand = '/Bitcoin Cash Node:29.1.0/'
        self.address_valid = True
        owner = self
        class RPC(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                if self.headers.get('Authorization') != 'Basic '+base64.b64encode(b'fixture:private-fixture').decode():
                    self.send_error(401); return
                data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                result = {'getnetworkinfo': {'subversion': owner.node_brand},
                          'getblockchaininfo': {'chain': 'regtest'},
                          'validateaddress': {'isvalid': owner.address_valid},
                          'getblocktemplate': {'height': 101}}[data['method']]
                body = json.dumps({'result': result, 'error': None}).encode()
                self.send_response(200); self.end_headers(); self.wfile.write(body)
            def log_message(self, *args): pass
        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), RPC)
        thread = threading.Thread(target=self.server.serve_forever, daemon=True); thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def run_solo(self, rpc_url=None):
        env = os.environ.copy(); env['CASHSTRATUM_RPC_PASSWORD'] = 'private-fixture'
        env['PATH'] = str(self.commands)+os.pathsep+env['PATH']
        return subprocess.run(['bash', str(REPO/'scripts/install-cashstratum-solo.sh'),
            '--source-dir', str(self.source), '--destdir', str(self.stage), '--no-start',
            '--user', pwd.getpwuid(os.getuid()).pw_name, '--rpc-user', 'fixture',
            '--rpc-url', rpc_url or f'127.0.0.1:{self.server.server_port}', '--address', ADDRESS],
            env=env, text=True, capture_output=True)

    def test_fresh_and_rerun_preserve_config_and_legacy_evidence(self):
        legacy = self.stage/'opt/ckpool/logs'; legacy.mkdir(parents=True)
        evidence = legacy/'solve.log'; evidence.write_text('historical solve\n')
        first = self.run_solo(); self.assertEqual(first.returncode, 0, first.stderr)
        target = self.stage/'opt/cashstratum'
        config = target/'cashstratum.conf'
        original = config.read_bytes()
        self.assertEqual(json.loads(original)['bchaddress'], ADDRESS)
        self.assertEqual(config.stat().st_mode & 0o777, 0o600)
        unit = (self.stage/'etc/systemd/system/cashstratum.service').read_text()
        self.assertIn('-n cashstratum -B -L', unit)
        self.assertNotIn('PrivateTmp=true', unit)
        self.assertFalse((target/'ckpool').exists())
        data = json.loads(original); data['poolfee'] = 1.25
        config.write_text(json.dumps(data, indent=4)+'\n'); original = config.read_bytes()
        second = self.run_solo(); self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(config.read_bytes(), original)
        self.assertEqual(evidence.read_text(), 'historical solve\n')
        self.assertNotIn('private-fixture', first.stdout+first.stderr+second.stdout+second.stderr)

    def test_failed_build_leaves_installed_binary_untouched(self):
        first = self.run_solo(); self.assertEqual(first.returncode, 0, first.stderr)
        binary = self.stage/'opt/cashstratum/cashstratum'
        binary.write_bytes(b'previous working binary')
        (self.source/'Makefile').write_text('all:\n\t@false\n')
        result = self.run_solo()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(binary.read_bytes(), b'previous working binary')

    def test_bitcoin_core_rejected_before_install(self):
        self.node_brand = '/Satoshi:31.1.0/'
        result = self.run_solo()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.stage/'opt/cashstratum').exists())

    def test_invalid_payout_rejected_before_install(self):
        self.address_valid = False
        result = self.run_solo()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.stage/'opt/cashstratum').exists())

    def test_solo_rejects_unsupported_rpc_urls_before_install(self):
        host = f'127.0.0.1:{self.server.server_port}'
        for url in ('https://'+host, 'http://'+host+'/', host+'/wallet/pool',
                    host+'?query=1', host+'#fragment', 'http://user:pass@'+host,
                    host+'?', host+'#', 'http://127.0.0.1', 'http://'+host+'\n'):
            with self.subTest(url=url):
                result = self.run_solo(url)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.stage/'opt/cashstratum').exists())

    def test_solo_rejects_unsupported_existing_fallback_rpc(self):
        self.assertEqual(self.run_solo().returncode, 0)
        target = self.stage/'opt/cashstratum'
        config = target/'cashstratum.conf'
        data = json.loads(config.read_text())
        data['btcd'].append(dict(data['btcd'][0], url='https://127.0.0.1:8332'))
        config.write_text(json.dumps(data))
        before = config.read_bytes()
        (target/'cashstratum').write_bytes(b'previous working executable')
        result = self.run_solo()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(config.read_bytes(), before)
        self.assertEqual((target/'cashstratum').read_bytes(), b'previous working executable')

    def test_proxy_eof_before_inputs_never_stops_service(self):
        source = (REPO/'scripts/install-csproxy.sh').read_text()
        start = source.index('# Prompt for upstream pools')
        end = source.index('# Prepare private configuration', start)
        script = 'systemctl() { echo UNEXPECTED_SERVICE_CHANGE; };\n'+source[start:end]
        result = subprocess.run(['bash', '-e', '-c', script], stdin=subprocess.DEVNULL,
                                text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('UNEXPECTED_SERVICE_CHANGE', result.stdout)
        self.assertGreater(source.index('systemctl stop csproxy; fi'), end)

    def proxy_transaction(self, fail_start=False):
        source = (REPO/'scripts/install-csproxy.sh').read_text()
        build = self.root/'proxy-build'; build.mkdir()
        (build/'csproxy.conf').write_text('{"private":"new secret"}\n')
        (build/'csproxy.service').write_text('new service\n')
        conf = self.root/'config/csproxy.conf'; conf.parent.mkdir()
        conf.write_text('old private config\n'); conf.chmod(0o600)
        unit = self.root/'csproxy.service'; unit.write_text('old unit\n')
        binary = self.root/'bin/csproxy'; binary.parent.mkdir()
        old_binary = binary.parent/'ckpool'; old_binary.write_text('old binary\n')
        binary.symlink_to('ckpool')
        trace = self.root/'systemctl-trace'
        logs = self.root/'logs'; logs.mkdir(); logs.chmod(0o750)
        env = os.environ.copy()
        env.update(BUILD_DIR=str(build), SRC_DIR=str(self.source), CONF_DIR=str(conf.parent),
                   CONF_FILE=str(conf), LOG_DIR=str(self.root/'logs'), SERVICE_FILE=str(unit),
                   service_user=pwd.getpwuid(os.getuid()).pw_name, TRACE=str(trace),
                   FAIL_START='yes' if fail_start else 'no', FAILED=str(self.root/'failed-once'))
        start = source.index('# Keep a rollback copy')
        end = source.index('\necho\necho "Installation complete!', start)
        transaction = source[start:end].replace('BINARY_FILE=/usr/local/bin/csproxy',
                                                'BINARY_FILE="$TEST_BINARY"')
        env['TEST_BINARY'] = str(binary)
        stubs = '''
systemctl() {
    echo "$*" >> "$TRACE"
    if [[ "$1" == start && "$FAIL_START" == yes && ! -e "$FAILED" ]]; then
        touch "$FAILED"; return 1
    fi
    return 0
}
sleep() { :; }
chown() {
    command chown "$@"
    # Simulate an account-change side effect on directory permissions too.
    if [[ "${*: -1}" == "$LOG_DIR" ]]; then chmod 700 "$LOG_DIR"; fi
}
'''
        result = subprocess.run(['bash', '-e', '-c', stubs+transaction],
                                env=env, text=True, capture_output=True)
        return result, binary, conf, unit, trace

    def test_proxy_atomic_private_config_and_dedicated_binary(self):
        result, binary, conf, unit, trace = self.proxy_transaction()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(conf.stat().st_mode & 0o777, 0o600)
        self.assertFalse(binary.is_symlink())
        self.assertEqual((binary.parent/'ckpool').read_text(), 'old binary\n')
        self.assertIn('new secret', conf.read_text())
        self.assertIn('start csproxy', trace.read_text())

    def test_proxy_failed_activation_restores_artifacts_and_running_state(self):
        result, binary, conf, unit, trace = self.proxy_transaction(fail_start=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(binary.is_symlink())
        self.assertEqual(os.readlink(binary), 'ckpool')
        self.assertEqual(conf.read_text(), 'old private config\n')
        self.assertEqual(unit.read_text(), 'old unit\n')
        self.assertEqual(conf.stat().st_mode & 0o777, 0o600)
        self.assertEqual(trace.read_text().count('start csproxy'), 2)
        self.assertIn('enable csproxy', trace.read_text())
        metadata = (self.root/'logs').stat()
        self.assertEqual(metadata.st_mode & 0o777, 0o750)
        self.assertEqual(metadata.st_uid, os.getuid())
        self.assertEqual(metadata.st_gid, self.root.stat().st_gid)

    def solo_transaction(self, fail_activation):
        """Run the solo installer's install+activate transaction against stubs.

        Mirrors proxy_transaction: executes the real script slices (rollback
        handler + install transaction) so the failure path is exercised as
        shipped, not re-implemented in the test.
        """
        source = (REPO/'scripts/install-cashstratum-solo.sh').read_text()
        handler_start = source.index('# Set once the previous binaries have been saved')
        handler = source[handler_start:source.index('trap cleanup EXIT', handler_start)+len('trap cleanup EXIT')]
        body_start = source.index('# Save the binaries we are about to replace')
        # Include the `rollback_needed=false` disarm, otherwise the EXIT trap
        # correctly rolls back a successful install and the slice misreports.
        body = source[body_start:source.index("printf 'CashStratum installed at", body_start)]

        work = self.root/'work'; (work/'source/src').mkdir(parents=True)
        for name in ('ckpool', 'ckpmsg', 'notifier'):
            binary = work/'source/src'/name
            binary.write_text('new %s\n' % name); binary.chmod(0o755)
        target = self.root/'opt-cashstratum'; target.mkdir()
        # The install we must be able to get back to.
        for name in ('cashstratum', 'ckpmsg', 'notifier'):
            previous = target/name
            previous.write_text('old %s\n' % name); previous.chmod(0o755)
        (target/'cashstratum.conf').write_text('old config\n')
        systemd = self.root/'systemd'; systemd.mkdir()
        trace = self.root/'solo-systemctl-trace'
        env = os.environ.copy()
        env.update(WORK_DIR=str(work), TARGET=str(target), DESTDIR='', START='true',
                   INSTALL_DIR=str(target), SYSTEMD_DIR=str(systemd), TRACE=str(trace),
                   SERVICE_USER=pwd.getpwuid(os.getuid()).pw_name,
                   SEEN_ACTIVE=str(self.root/'seen-active'),
                   FAIL_ACTIVATION='yes' if fail_activation else 'no')
        stubs = '''
systemctl() {
    echo "$*" >> "$TRACE"
    if [[ "$1" == is-active ]]; then
        # First call detects the pre-existing running pool; the second is the
        # post-restart health check that we make fail.
        if [[ -e "$SEEN_ACTIVE" && "$FAIL_ACTIVATION" == yes ]]; then return 1; fi
        touch "$SEEN_ACTIVE"; return 0
    fi
    return 0
}
sleep() { :; }
'''
        result = subprocess.run(['bash', '-euo', 'pipefail', '-c', stubs+handler+'\n'+body],
                                env=env, text=True, capture_output=True)
        return result, target, trace

    def test_solo_failed_activation_restores_previous_binaries(self):
        result, target, trace = self.solo_transaction(fail_activation=True)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        # Every replaced binary is back to the version that was running.
        for name in ('cashstratum', 'ckpmsg', 'notifier'):
            self.assertEqual((target/name).read_text(), 'old %s\n' % name,
                             '%s was not restored' % name)
        # The pool that was stopped is started again.
        self.assertIn('start cashstratum.service', trace.read_text())
        self.assertIn('previous binaries and service state restored', result.stderr)
        # The operator is told how to see why it failed.
        self.assertIn('journalctl -u cashstratum', result.stderr)

    def test_solo_successful_activation_keeps_new_binaries(self):
        result, target, trace = self.solo_transaction(fail_activation=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((target/'cashstratum').read_text(), 'new ckpool\n')
        self.assertEqual((target/'ckpmsg').read_text(), 'new ckpmsg\n')
        self.assertNotIn('restored', result.stderr)
        # An existing configuration is never overwritten by a reinstall.
        self.assertEqual((target/'cashstratum.conf').read_text(), 'old config\n')

    def test_launcher_resolves_its_own_directory_and_runtime_argument(self):
        source = (REPO/'install-cashstratum.sh').read_text()
        start = source.index('cat > "$INSTALL_DIR/start-cashstratum.sh"')
        end = source.index('chmod +x "$INSTALL_DIR/start-cashstratum.sh"', start)
        env = os.environ.copy(); env['INSTALL_DIR'] = str(self.root)
        subprocess.run(['bash', '-c', source[start:end]], env=env, check=True)
        daemon = self.root/'cashstratum'
        daemon.write_text('#!/bin/sh\nprintf "%s\\n" "$PWD" "$@"\n'); daemon.chmod(0o755)
        result = subprocess.run(['bash', str(self.root/'start-cashstratum.sh'), 'custom.conf'],
            cwd='/', text=True, capture_output=True, check=True)
        self.assertIn(str(self.root)+'\n-c\ncustom.conf\n-n\ncashstratum\n-L\n-B\n', result.stdout)

    def test_proxy_rejects_sv2_scheme_and_escapes_json(self):
        source = (REPO/'scripts/install-csproxy.sh').read_text()
        start = source.index('parse_pool_url() {')
        end = source.index('\n}', start)+2
        parse = source[start:end]
        start = source.index('    if [[ "$PARSED_SV2" == true')
        end = source.index('    if ! test_pool', start)
        reject = source[start:end]
        script = parse+'\nfor pool_url in "$@"; do\nparse_pool_url "$pool_url" || exit 1\n'+reject+'echo ACCEPTED\ndone\n'
        result = subprocess.run(['bash', '-c', script, 'test',
            'stratum2+tcp://localhost:3333', 'localhost:3333/KEY',
            'stratum+tcp://localhost:3333'], text=True, capture_output=True, check=True)
        self.assertEqual(result.stdout.count('ACCEPTED'), 1)
        self.assertEqual(result.stdout.count('Stratum V1 only'), 2)
        start = source.index('json_escape() {'); end = source.index('\n}', start)+2
        value = 'worker"\\tab\tline\n'
        result = subprocess.run(['bash', '-c', source[start:end]+'\njson_escape "$1"',
            'test', value], text=True, capture_output=True, check=True)
        self.assertEqual(json.loads('"'+result.stdout.rstrip('\n')+'"'), value)

    def test_interactive_fresh_config_escapes_credentials(self):
        source = (REPO/'install-cashstratum.sh').read_text()
        start = source.index('# Preserve configured nodes')
        end = source.index('# Create start script', start)
        env = os.environ.copy(); env['INSTALL_DIR'] = str(self.root)
        password = 'quote"and\\slash'
        result = subprocess.run(['bash', '-e', '-c', source[start:end]], env=env,
            input='127.0.0.1:8332\noperator\n'+password+'\nbitcoincash:qexample\n',
            text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads((self.root/'cashstratum.conf').read_text())
        self.assertEqual(data['btcd'][0]['pass'], password)
        self.assertEqual(data['poolfee'], 0)
        self.assertNotIn(password, result.stdout+result.stderr)

    def test_interactive_config_is_private_during_first_write(self):
        source = (REPO/'install-cashstratum.sh').read_text()
        start = source.index('# Preserve configured nodes')
        end = source.index('# Create start script', start)
        recorder = self.root/'recording-cat.py'
        recorder.write_text('import os,pathlib,shutil,sys\n'
                            'pathlib.Path(os.environ["MODE_OUTPUT"]).write_text(oct(os.fstat(1).st_mode & 0o777))\n'
                            'shutil.copyfileobj(sys.stdin,sys.stdout)\n')
        env = dict(os.environ, INSTALL_DIR=str(self.root), RECORDING_CAT=str(recorder),
                   MODE_OUTPUT=str(self.root/'creation-mode'))
        prefix = 'umask 022; cat() { python3 "$RECORDING_CAT"; };\n'
        result = subprocess.run(['bash', '-e', '-c', prefix+source[start:end]], env=env,
            input='127.0.0.1:8332\noperator\nsecret-fixture\nbitcoincash:qexample\n',
            text=True,capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root/'creation-mode').read_text(), '0o600')

    def test_interactive_installer_preserves_existing_config(self):
        source = (REPO/'install-cashstratum.sh').read_text()
        start = source.index('# Preserve configured nodes')
        end = source.index('# Create start script', start)
        config = self.root/'cashstratum.conf'; config.write_text('operator owned bytes\n')
        env = os.environ.copy(); env['INSTALL_DIR'] = str(self.root)
        result = subprocess.run(['bash', '-e', '-c', source[start:end]], env=env,
                                stdin=subprocess.DEVNULL, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config.read_text(), 'operator owned bytes\n')

if __name__ == '__main__': unittest.main(verbosity=2)
