#!/usr/bin/env python3
"""Offline recovery failures must never degrade into downloads or bad inputs."""
import hashlib
import importlib.util
import io
import json
import os
import shutil
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


sources = load('preserved-sources')
vaults = load('dependency-vault')


class Preservation(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name).resolve()
        self.root = self.tmp / 'repo'
        (self.root / 'third_party').mkdir(parents=True)
        (self.root / 'docs').mkdir()
        (self.root / 'docs/DSP_FIRMWARE_PROVENANCE.md').write_text('fixture provenance')
        self.vault = self.tmp / 'vault'

    def fixture(self):
        p = self.root / 'out/deps/a'
        p.parent.mkdir(parents=True)
        p.write_bytes(b'fixed dependency')
        lock = {'version': 1, 'files': [{'path': 'out/deps/a', 'sha256': hashlib.sha256(p.read_bytes()).hexdigest()}]}
        (self.root / 'third_party/recovery.lock.json').write_text(json.dumps(lock))
        return p

    def test_cold_restore_does_not_need_upstream(self):
        p = self.fixture()
        self.assertEqual(vaults.run('capture', self.vault, self.root), 1)
        p.unlink()
        vaults.run('restore', self.vault, self.root)
        self.assertEqual(p.read_bytes(), b'fixed dependency')
        with self.assertRaises(ValueError):
            vaults.run('capture', self.vault, self.root)

    def test_bad_vault_preserves_existing_files(self):
        p = self.fixture()
        vaults.run('capture', self.vault, self.root)
        (self.vault / 'out/deps/a').write_bytes(b'bad')
        with self.assertRaises(ValueError):
            vaults.run('restore', self.vault, self.root)
        self.assertEqual(p.read_bytes(), b'fixed dependency')

    def test_lock_tampering_and_destination_links_rejected(self):
        p = self.fixture()
        vaults.run('capture', self.vault, self.root)
        p.unlink()
        p.symlink_to(self.tmp / 'outside')
        with self.assertRaises(ValueError):
            vaults.run('restore', self.vault, self.root)
        self.assertFalse((self.tmp / 'outside').exists())
        (self.vault / 'recovery.lock.json').write_text('{}')
        with self.assertRaises(ValueError):
            vaults.run('verify', self.vault, self.root)

    def test_repository_vendor_hash_and_module_pins(self):
        sources.unpack('plugin-vendor', self.tmp / 'ok', ROOT)
        self.assertTrue((self.tmp / 'ok/vendor/modules.txt').is_file())
        module = self.tmp / 'module'
        module.mkdir()
        (module / 'go.mod').write_text('changed')
        with self.assertRaises(ValueError):
            sources.unpack('plugin-vendor', self.tmp / 'fail', module)
        self.assertFalse((self.tmp / 'fail').exists())

    def test_archive_traversal_rejected_even_with_matching_hash(self):
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode='w:gz') as archive:
            info = tarfile.TarInfo('../outside')
            info.size = 1
            archive.addfile(info, io.BytesIO(b'x'))
        data = buf.getvalue()
        (self.root / 'third_party/bad.tar.gz').write_bytes(data)
        lock = {'version': 1, 'archives': {'plugin-vendor': {'file': 'bad.tar.gz', 'sha256': hashlib.sha256(data).hexdigest()}}}
        (self.root / 'third_party/sources.lock.json').write_text(json.dumps(lock))
        with self.assertRaises(ValueError):
            sources.unpack('plugin-vendor', self.tmp / 'extract', root=self.root)
        self.assertFalse((self.tmp / 'outside').exists())

    def test_empty_offline_fetches_fail_without_network(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        mock = self.tmp / 'mock'
        mock.mkdir()
        marker = self.tmp / 'network-used'
        for name in ('git', 'curl'):
            p = mock / name
            local_git = 'case "$*" in *clone*|*fetch*) ;; *) exec "$REAL_GIT" "$@" ;; esac\n' if name == 'git' else ''
            p.write_text('#!/bin/sh\n' + local_git + 'touch "$NETWORK_MARKER"\nexit 99\n')
            p.chmod(0o755)
        env = dict(os.environ, AIRSIM_OFFLINE='1', PATH=str(mock) + ':' + os.environ['PATH'], NETWORK_MARKER=str(marker), REAL_GIT=shutil.which('git'))
        for name in ('fetch-sources.sh', 'fetch-deps.sh', 'fetch-firmware.sh'):
            p = scripts / name
            p.write_text((ROOT / 'scripts' / name).read_text())
            result = subprocess.run(['sh', str(p)], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(marker.exists())
            self.assertIn(b'Offline', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
