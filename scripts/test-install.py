#!/usr/bin/env python3
"""Isolated installer contract tests: no network, no real service mutation."""
import hashlib,io,os,pathlib,subprocess,tarfile,tempfile,unittest
INSTALL=pathlib.Path(__file__).with_name('install.sh')
class Installer(unittest.TestCase):
 def test_verified_linux_and_corrupt_archive(self):
  with tempfile.TemporaryDirectory() as folder:
   root=pathlib.Path(folder);bin=root/'bin';bin.mkdir();release=root/'release';release.mkdir()
   name='pocket-agents-0.1.0-linux-arm64'
   payload=b'#!/bin/sh\nprintf "%s\\n" "$1" >> "$PROBE_LOG"\n'
   archive=release/(name+'.tar.gz')
   with tarfile.open(archive,'w:gz') as t:
    info=tarfile.TarInfo(name+'/pocket');info.size=len(payload);info.mode=0o700;t.addfile(info,io.BytesIO(payload))
   (release/'SHA256SUMS').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
   mocks={'id':'echo 501','uname':'if [ "$1" = -s ]; then echo Linux; else echo aarch64; fi','systemctl':'exit 0','curl':'''for arg in "$@"; do case "$arg" in https://*) url=$arg;; esac; done
while [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then out=$2;break;fi;shift;done
cp "$PROBE_RELEASE/${url##*/}" "$out"'''}
   for name,text in mocks.items():
    p=bin/name;p.write_text('#!/bin/sh\n'+text+'\n');p.chmod(0o700)
   env={**os.environ,'PATH':str(bin)+':/usr/bin:/bin','HOME':str(root),'PROBE_RELEASE':str(release),'PROBE_LOG':str(root/'executed')}
   result=subprocess.run(['sh',str(INSTALL),'--base-url','https://release.invalid/v0.1.0'],env=env,capture_output=True)
   self.assertEqual(result.returncode,0,result.stderr.decode());self.assertEqual((root/'executed').read_text(),'setup\n')
   (root/'executed').unlink();archive.write_bytes(archive.read_bytes()+b'corrupt')
   result=subprocess.run(['sh',str(INSTALL),'--base-url','https://release.invalid/v0.1.0'],env=env,capture_output=True)
   self.assertNotEqual(result.returncode,0);self.assertFalse((root/'executed').exists())
if __name__=='__main__':unittest.main()
