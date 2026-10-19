#!/usr/bin/env python3
"""Package already-built binaries, documentation and exact dependency notices."""
import hashlib
import pathlib
import tarfile

# Deliberate allowlist: never recursively ship operational evidence or diaries.
public_docs = ('LICENSE', 'THIRD_PARTY.md', 'AGENTS.md', 'CONTRIBUTING.md', 'README.md', 'README.zh-CN.md',
               'README.ja.md', 'docs/INSTALL.md',
               'docs/ARCHITECTURE.md', 'docs/COMPATIBILITY.md',
               'docs/ACCEPTANCE.md', 'docs/DECISIONS.md',
               'docs/assets/logo.svg', 'docs/assets/how-it-works.svg',
               'docs/assets/how-it-works-zh.svg', 'docs/assets/overview.png',
               'docs/ONBOARDING.md', 'docs/MACOS-SIGNING.md')
root = pathlib.Path(__file__).resolve().parents[1]
out = root / 'bin' / 'release'
out.mkdir(parents=True, exist_ok=True)
checksums = []
(out / 'install.sh').write_bytes((root / 'scripts' / 'install.sh').read_bytes())
for platform in ('darwin-arm64', 'linux-amd64', 'linux-arm64'):
    binary = root / 'bin' / ('pocket-' + platform)
    if not binary.is_file():
        raise SystemExit('Missing binary: run make cross first')
    name = 'pocket-agents-0.1.0-' + platform
    archive = out / (name + '.tar.gz')
    with tarfile.open(archive, 'w:gz') as bundle:
        bundle.add(binary, arcname=name + '/pocket')
        for item in public_docs + ('scripts',):
            bundle.add(root / item, arcname=name + '/' + item)
        bundle.add(root / 'bin' / 'licenses', arcname=name + '/licenses')
    checksums.append(hashlib.sha256(archive.read_bytes()).hexdigest() + '  ' + archive.name)
    print(archive)
name = 'pocket-agents-0.1.0-source'
archive = out / (name + '.tar.gz')

def source_only(info):
    parts = pathlib.PurePosixPath(info.name).parts
    if any(part in ('node_modules', 'dist', '__pycache__', '.DS_Store') for part in parts):
        return None
    if info.name.endswith(('.tsbuildinfo', '.pyc')):
        return None
    return info

with tarfile.open(archive, 'w:gz') as bundle:
    for item in public_docs + ('Makefile', 'go.mod', 'go.sum', 'cmd', 'internal', 'web', 'scripts'):
        bundle.add(root / item, arcname=name + '/' + item, filter=source_only)
checksums.append(hashlib.sha256(archive.read_bytes()).hexdigest() + '  ' + archive.name)
print(archive)
(out / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n')
