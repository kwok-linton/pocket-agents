#!/usr/bin/env python3
"""Exercise release contents in an isolated fixture, without building binaries."""
import ast
import pathlib
import shutil
import subprocess
import sys
import tarfile
import tempfile

script = pathlib.Path(__file__).with_name('package-release.py')
tree = ast.parse(script.read_text())
docs = next(ast.literal_eval(n.value) for n in tree.body
            if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'public_docs' for t in n.targets))
with tempfile.TemporaryDirectory() as temp:
    root = pathlib.Path(temp)
    for name in (*docs, 'AGENTS.md', 'Makefile', 'go.mod', 'go.sum', 'scripts/install.sh',
                 'bin/licenses/NOTICE', 'cmd/main.go', 'internal/example.go', 'web/package.json',
                 'PLAN.md', 'PROGRESS.md', 'SCOPE.md', 'docs/evidence/private.md', 'docs/private.md',
                 'web/node_modules/private.txt', 'web/dist/private.txt'):
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('fixture')
    for platform in ('darwin-arm64', 'linux-amd64', 'linux-arm64'):
        (root / 'bin' / ('pocket-' + platform)).write_text('fixture binary')
    shutil.copyfile(script, root / 'scripts/package-release.py')
    subprocess.run([sys.executable, str(root / 'scripts/package-release.py')], check=True, capture_output=True)
    archives = list((root / 'bin/release').glob('*.tar.gz'))
    assert len(archives) == 4
    for archive in archives:
        with tarfile.open(archive) as bundle:
            names = {'/'.join(pathlib.PurePosixPath(n).parts[1:]) for n in bundle.getnames()}
        assert set(docs) <= names, archive
        assert not any('private' in n or n in ('PLAN.md', 'PROGRESS.md', 'SCOPE.md') for n in names), archive
    print('PASS: four archives include all public languages and exclude private evidence/build output.')
