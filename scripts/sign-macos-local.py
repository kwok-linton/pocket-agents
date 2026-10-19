#!/usr/bin/env python3
"""Stable per-user development signing identity. Never installs a trust root.
For public distribution use an Apple Developer ID identity instead.
"""
import hashlib, os, pathlib, secrets, subprocess, sys, tempfile, shlex

def run(*args):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

def main():
    if sys.platform != 'darwin' or len(sys.argv) != 2:
        raise SystemExit('usage (macOS): sign-macos-local.py EXECUTABLE')
    target = pathlib.Path(sys.argv[1]).resolve(strict=True)
    directory = pathlib.Path.home()/'.pocket-agents-signing'
    directory.mkdir(mode=0o700, exist_ok=True)
    os.chmod(directory, 0o700)
    keychain = directory/'identity.keychain-db'
    secret = directory/'keychain-password'
    cert = directory/'identity.der'
    if not keychain.exists():
        if any(directory.iterdir()):
            raise SystemExit('Incomplete signing identity; inspect private signing directory rather than silently rotating it.')
        password = secrets.token_urlsafe(40)
        secret.write_text(password); secret.chmod(0o600)
        with tempfile.TemporaryDirectory(dir=directory) as tmp:
            tmp = pathlib.Path(tmp)
            conf = tmp/'openssl.cnf'
            conf.write_text('[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=Pocket Agents Local Development\n[ext]\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=critical,codeSigning\n')
            run('/usr/bin/openssl','req','-new','-x509','-newkey','rsa:3072','-nodes','-days','3650','-config',str(conf),'-keyout',str(tmp/'key.pem'),'-out',str(tmp/'cert.pem'))
            run('/usr/bin/openssl','x509','-in',str(tmp/'cert.pem'),'-outform','DER','-out',str(cert))
            run('/usr/bin/openssl','pkcs12','-export','-inkey',str(tmp/'key.pem'),'-in',str(tmp/'cert.pem'),'-out',str(tmp/'identity.p12'),'-passout','pass:'+password)
            run('/usr/bin/security','create-keychain','-p',password,str(keychain))
            run('/usr/bin/security','unlock-keychain','-p',password,str(keychain))
            run('/usr/bin/security','import',str(tmp/'identity.p12'),'-k',str(keychain),'-P',password,'-T','/usr/bin/codesign')

    keychain.chmod(0o600)
    password = secret.read_text()
    fingerprint = hashlib.sha1(cert.read_bytes()).hexdigest().upper()
    run('/usr/bin/security','unlock-keychain','-p',password,str(keychain))
    previous = shlex.split(run('/usr/bin/security','list-keychains','-d','user').decode())
    try:
        run('/usr/bin/security','list-keychains','-d','user','-s',*previous,str(keychain))
        run('/usr/bin/codesign','--force','--sign',fingerprint,'--keychain',str(keychain),'--identifier','local.pocket-agents','--timestamp=none',str(target))
    finally:
        run('/usr/bin/security','list-keychains','-d','user','-s',*previous)
    run('/usr/bin/codesign','--verify','--strict',str(target))
    requirement = subprocess.run(['/usr/bin/codesign','-d','-r-',str(target)],capture_output=True,check=True).stdout.decode()
    print(requirement.strip())
    print('Signed and verified with persistent local identity. No trust settings changed.')

if __name__ == '__main__':
    try: main()
    except subprocess.CalledProcessError as e:
        # Never print command arguments: keychain/import passwords are arguments.
        raise SystemExit('Signing failed: '+e.stderr.decode(errors='replace'))
