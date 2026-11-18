"""Build a read-only installer disk with the verified upstream app archive."""
from pathlib import Path
import shutil, subprocess, sys, hashlib


src = Path(__file__).resolve().parent
folder, archive, iso = map(Path, sys.argv[1:])
with archive.open('rb') as fp:
    if hashlib.file_digest(fp, 'sha256').hexdigest() != 'fb321bd9832403cf766e3e9719c2063cee1747a320925d3aecd4f0ce67ca6013':
        raise ValueError('Antigravity archive checksum mismatch')
folder.mkdir(parents=True, exist_ok=True)
for name in ('install.sh','relay.py'):
    shutil.copyfile(src/'guest'/name,folder/name)
if archive.resolve() != (folder/'app.tgz').resolve():
    shutil.copyfile(archive,folder/'app.tgz')
subprocess.run(['hdiutil','makehybrid','-iso','-joliet','-default-volume-name','CIDATA','-o',str(iso),str(folder)],check=True)
