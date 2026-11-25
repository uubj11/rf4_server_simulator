"""Release staging excludes runtime and player files."""
from pathlib import Path
import tempfile
import unittest
from stage_preview import allowed, optional_local_files, release_resources, stage

class StagingTests(unittest.TestCase):
    def test_only_allowed_resources_are_copied(self):
        keep = ['README.md', 'LICENSE', 'config.example.yaml', 'server_data/static.sqlite3',
                'server_data/compat_parts.json', 'docs/deploy.md', 'local-service/local_server.py',
                'local-service/web/portal/local.js', 'local-service/rf4_local_membership.py',
                'ref/icons.zip',
                *['internal/portal/web/'+name for name in ('framework.css','portal.css','reference-ui.css','local.css')]]
        reject = ['Game/rf4_x64.exe', 'local-service/data/gameplay.sqlite3',
                  'local-service/data/characters/inventory.json', 'local-service/__pycache__/x.pyc',
                  'local-service/tests/test_backend.py', 'server_data/player.sqlite3',
                  'docs/player.db', 'logs/run.log', 'profile/settings.json',
                  'internal/portal/web/index.html', 'internal/portal/web/local.js', 'internal/portal/web/extra.css',
                  'local-service/web/portal/local.css', 'ref/paper_maps/map_unapproved.webp']
        with tempfile.TemporaryDirectory(prefix='rf4-stage-') as tmp:
            root, target = Path(tmp)/'source', Path(tmp)/'package'
            for name in keep + reject:
                p = root/name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_text('synthetic', encoding='utf-8')
            stage(root, target, keep + reject)
            self.assertEqual(set(keep), {p.relative_to(target).as_posix() for p in target.rglob('*') if p.is_file()})
            self.assertEqual((target/'server_data/static.sqlite3').read_text(), 'synthetic')
        self.assertFalse(allowed('../README.md'))

    def test_optional_icon_archive_is_staged_only_when_present(self):
        with tempfile.TemporaryDirectory(prefix='rf4-stage-') as tmp:
            root = Path(tmp)
            self.assertEqual(optional_local_files(root), [])
            archive = root/'ref/icons.zip'
            archive.parent.mkdir(parents=True)
            archive.write_bytes(b'zip')
            self.assertEqual(optional_local_files(root), ['ref/icons.zip'])
            self.assertTrue(allowed('ref/icons.zip'))

    def test_optional_icon_archive_size_is_checked(self):
        with tempfile.TemporaryDirectory(prefix='rf4-stage-') as tmp:
            root = Path(tmp)
            archive = root/'ref/icons.zip'
            archive.parent.mkdir(parents=True)
            with archive.open('wb') as fh:
                fh.truncate((128 << 20) + 1)
            with self.assertRaises(ValueError):
                optional_local_files(root)

    def test_release_resources_merge_tracked_and_optional_files(self):
        with tempfile.TemporaryDirectory(prefix='rf4-stage-') as tmp:
            root = Path(tmp)
            archive = root/'ref/icons.zip'
            archive.parent.mkdir(parents=True)
            archive.write_bytes(b'zip')
            self.assertEqual(
                release_resources(root, ['README.md', '', 'README.md']),
                ['README.md', 'ref/icons.zip'],
            )

if __name__ == '__main__':
    unittest.main()
