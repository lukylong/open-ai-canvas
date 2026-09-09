from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent

class PreserveWebAssetsTest(unittest.TestCase):
    def test_two_releases_preserve_old_chunks_and_nested_dependencies(self):
        with tempfile.TemporaryDirectory() as folder:
            base = Path(folder)
            source, target = base / 'build', base / 'persistent'
            (source / 'nested').mkdir(parents=True)
            (source / 'home-old.js').write_text('export default 1')
            (source / 'nested/old.css').write_text('body {}')
            env = dict(os.environ, CANVAS_STATIC_SOURCE=str(source), CANVAS_STATIC_ROOT=str(target))
            run = lambda: subprocess.run(['sh', str(ROOT / 'scripts/preserve-web-assets.sh')], env=env, check=True)
            run()
            (source / 'home-old.js').unlink()
            (source / 'nested/old.css').unlink()
            (source / 'home-new.js').write_text('export default 2')
            run()
            run()
            self.assertEqual((target / 'assets/home-old.js').read_text(), 'export default 1')
            self.assertEqual((target / 'assets/home-new.js').read_text(), 'export default 2')
            self.assertEqual((target / 'assets/nested/old.css').read_text(), 'body {}')
            self.assertFalse((target / 'index.html').exists())

    def test_existing_hashed_asset_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as folder:
            base = Path(folder)
            (base / 'build').mkdir()
            (base / 'persistent/assets').mkdir(parents=True)
            (base / 'build/chunk.js').write_text('new')
            (base / 'persistent/assets/chunk.js').write_text('old')
            env = dict(os.environ, CANVAS_STATIC_SOURCE=str(base / 'build'), CANVAS_STATIC_ROOT=str(base / 'persistent'))
            subprocess.run(['sh', str(ROOT / 'scripts/preserve-web-assets.sh')], env=env, check=True)
            self.assertEqual((base / 'persistent/assets/chunk.js').read_text(), 'old')

    def test_deployment_contract(self):
        for name in ['docker-compose.local.yml', 'docker-compose.deploy.yml', 'docker-compose.server.yml']:
            self.assertIn('web-static-assets:/var/lib/canvas-static', (ROOT / name).read_text())
        self.assertIn('/docker-entrypoint.d/25-preserve-web-assets.sh', (ROOT / 'Dockerfile').read_text())
        config = (ROOT / 'nginx.conf').read_text()
        assets = config.split('location ^~ /assets/ {')[1].split('}')[0]
        self.assertIn('try_files $uri =404', assets)
        self.assertNotIn('/index.html', assets)
        self.assertIn('immutable', assets)
        self.assertIn('Cache-Control "no-cache"', config)

if __name__ == '__main__':
    unittest.main()
