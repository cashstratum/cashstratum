#!/usr/bin/env python3
"""Exercise generated cron commands and install hooks in disposable directories."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class RuntimeRegression(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='cashstratum-runtime-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_custom_directory_cron_is_quoted_and_preserves_other_schedules(self):
        text = (ROOT / 'post-install.sh').read_text()
        start = text.index('        # Cron uses /bin/sh')
        end = text.index('        echo -e', start)
        fragment = text[start:end]
        commands = self.root / 'commands'
        commands.mkdir()
        table = self.root / 'crontab'
        unrelated = '0 3 * * * /opt/other-pool/clean-old-blocks.sh\n'
        table.write_text(unrelated)
        fake = commands / 'crontab'
        fake.write_text('#!/bin/sh\ncase "$*" in\n *-l*) cat "$TEST_CRONTAB";;\n *) cat > "$TEST_CRONTAB.new"; mv "$TEST_CRONTAB.new" "$TEST_CRONTAB";;\nesac\n')
        fake.chmod(0o755)
        install = self.root / "pool's test%name"
        install.mkdir()
        script = install / 'clean-old-blocks.sh'
        marker = self.root / 'invocation'
        script.write_text('#!/bin/sh\nprintf "%s" "$CASHSTRATUM_DIR" > "$TEST_INVOCATION"\n')
        table.write_text(unrelated + f'0 3 * * * {script}\n')
        env = dict(os.environ, PATH=str(commands)+':'+os.environ['PATH'],
                   INSTALL_DIR=str(install), CLEANUP_SCRIPT=str(script), ACTUAL_USER='operator',
                   TEST_CRONTAB=str(table), TEST_INVOCATION=str(marker))
        for _ in range(2):
            result = subprocess.run(['bash', '-e', '-c', fragment], env=env, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
        lines = table.read_text().splitlines()
        self.assertEqual(len(lines), 2)
        self.assertEqual(lines[0]+'\n', unrelated)
        # Cron unescapes escaped percent signs before invoking /bin/sh.
        command = lines[1].split(' ', 5)[5].replace('\\%', '%')
        result = subprocess.run(['/bin/sh', '-c', command], env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(marker.read_text(), str(install))

    def hook_makefile(self, bindir):
        source = (ROOT / 'src/Makefile.am').read_text()
        recipes = []
        for target in ('install-exec-hook', 'uninstall-local'):
            match = re.search(r'^'+target+r':\n(?:\t.*\n)+', source, re.M)
            self.assertIsNotNone(match)
            recipes.append(match.group())
        # Paths with spaces exercise the recipe quoting without depending on
        # automake's platform-specific full build prerequisites.
        makefile = self.root / 'Makefile'
        makefile.write_text('LN_S = ln -s\nINSTALL_BIND_CAPABILITY = no\n'
                            'bindir = '+str(bindir)+'\n'+''.join(recipes))
        commands = self.root / 'commands'
        commands.mkdir()
        fake = commands / 'setcap'
        fake.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$TEST_CAP_LOG"\n')
        fake.chmod(0o755)
        return dict(os.environ, PATH=str(commands)+':'+os.environ['PATH'], TEST_CAP_LOG=str(self.root/'caps'))

    def run_make(self, env, *args):
        result = subprocess.run(['make', '-f', str(self.root/'Makefile'), *args],
                                env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_staged_install_and_uninstall_never_touch_host_bindir(self):
        bindir = self.root / 'host bin'
        bindir.mkdir()
        (bindir/'ckpool').write_text('host binary')
        host_alias = bindir/'cashstratum'
        host_alias.write_text('host alias')
        stage = self.root/'stage root'
        staged_bin = Path(str(stage)+str(bindir))
        staged_bin.mkdir(parents=True)
        (staged_bin/'ckpool').write_text('staged binary')
        env = self.hook_makefile(bindir)
        self.run_make(env, 'DESTDIR='+str(stage), 'INSTALL_BIND_CAPABILITY=yes', 'install-exec-hook')
        for alias in ('cashstratum','ckproxy','csproxy'):
            self.assertEqual(os.readlink(staged_bin/alias), 'ckpool')
        self.assertFalse((self.root/'caps').exists())
        self.run_make(env, 'DESTDIR='+str(stage), 'uninstall-local')
        self.assertFalse((staged_bin/'cashstratum').exists())
        self.assertEqual(host_alias.read_text(), 'host alias')

    def test_capabilities_are_opt_in_for_nonstaged_install(self):
        bindir = self.root / 'bin'
        bindir.mkdir()
        (bindir/'ckpool').write_text('binary')
        env = self.hook_makefile(bindir)
        self.run_make(env, 'install-exec-hook')
        self.assertFalse((self.root/'caps').exists())
        self.run_make(env, 'INSTALL_BIND_CAPABILITY=yes', 'install-exec-hook')
        self.assertIn(str(bindir/'ckpool'), (self.root/'caps').read_text())


if __name__ == '__main__':
    unittest.main()
