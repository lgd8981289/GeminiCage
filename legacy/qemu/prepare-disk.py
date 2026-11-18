"""Prepare a fresh verified Debian cloud image for a guest without a NIC."""
import hashlib
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import tempfile
import struct
import zlib

image, disk = map(Path, sys.argv[1:3])
expected = '3be884e19878b307a5916eb59be17efb7f1a5ec8290f2253345640f0a8740082fd40cfb7a7940b56cca0a76289ecd448507011cfd1b17472c9998d90f44b5c2c'
with image.open('rb') as fp:
    if hashlib.file_digest(fp, 'sha512').hexdigest() != expected:
        raise ValueError('Debian image checksum mismatch')
if disk.exists():
    raise FileExistsError(disk)
os.umask(0o077)
run = subprocess.run
qemu = shutil.which('qemu-img') or '/Applications/Docker.app/Contents/MacOS/qemu-img'
debugfs = '/opt/homebrew/opt/e2fsprogs/sbin/debugfs'
run([qemu,'convert','-O','raw',image,disk],check=True)
run([qemu,'resize','-f','raw',disk,'28G'],check=True)
# Extend the last data partition before Linux boots. Keep EFI and all GUIDs.
with disk.open('r+b') as fp:
    last = disk.stat().st_size//512 - 1
    fp.seek(512)
    header = bytearray(fp.read(512))
    if header[:8] != b'EFI PART':
        raise ValueError('missing GPT')
    size = struct.unpack_from('<I',header,12)[0]
    table_sector, count, entry_size = struct.unpack_from('<QII',header,72)
    fp.seek(table_sector*512)
    table = bytearray(fp.read(count*entry_size))
    sectors = (len(table)+511)//512
    boundary = last-sectors-1
    start,end = struct.unpack_from('<QQ',table,32)
    if start != 262144 or not start < end < boundary:
        raise ValueError('unexpected verified Debian layout')
    struct.pack_into('<Q',table,40,boundary)
    struct.pack_into('<Q',header,48,boundary)
    struct.pack_into('<I',header,88,zlib.crc32(table))
    for here,backup,location in ((1,last,table_sector),(last,1,last-sectors)):
        struct.pack_into('<QQ',header,24,here,backup)
        struct.pack_into('<Q',header,72,location)
        struct.pack_into('<I',header,16,0)
        struct.pack_into('<I',header,16,zlib.crc32(header[:size]))
        fp.seek(location*512)
        fp.write(table)
        fp.seek(here*512)
        fp.write(header)
info = plistlib.loads(subprocess.check_output(['hdiutil','attach','-plist','-nomount','-imagekey','diskimage-class=CRawDiskImage',disk]))
base = next(e['dev-entry'] for e in info['system-entities'] if re.fullmatch(r'/dev/disk[0-9]+',e.get('dev-entry','')))
part = base+'s1'
with tempfile.TemporaryDirectory() as tmp:
    try:
        sb = Path(debugfs).parent
        for cmd in ([sb/'e2fsck','-f','-p',part],[sb/'resize2fs',part],[sb/'e2fsck','-f','-p',part]):
            code=run(cmd).returncode
            if code != 0 and not (Path(cmd[0]).name=='e2fsck' and code==1):
                raise RuntimeError('ext4 preparation failed')
        def read(cmd):
            return subprocess.check_output([debugfs,'-R',cmd,part]).decode()
        grub = read('cat /boot/grub/grub.cfg')
        grub, n = re.subn(r'(\n\s*linux\s+[^\n]+)',r'\1 console=tty0 console=hvc0 systemd.show_status=yes',grub)
        if n == 0:
            raise ValueError('missing Linux GRUB entry')
        paths = {}
        def guest_file(path, content):
            local = Path(tmp) / ('file'+str(len(paths)))
            local.write_text(content)
            paths[path] = local
        guest_file('/boot/grub/grub.cfg',grub)
        guest_file('/etc/fstab', read('cat /etc/fstab').replace(',discard',',nodiscard').replace(',x-systemd.growfs',''))
        guest_file('/etc/cloud/cloud-init.disabled','Gemini Cage uses offline provisioning.\n')
        guest_file('/etc/systemd/system/gemini-install.service',"""[Unit]
Description=Prepare isolated Antigravity desktop
After=local-fs.target systemd-user-sessions.service
ConditionPathExists=!/var/lib/gemini-ready
[Service]
Type=oneshot
ExecStart=/bin/bash -ec 'mkdir -p /mnt/gemini; mountpoint -q /mnt/gemini || mount -o ro /dev/vdb /mnt/gemini; exec bash /mnt/gemini/install.sh'
TimeoutStartSec=infinity
StandardOutput=journal+console
StandardError=journal+console
[Install]
WantedBy=multi-user.target
""")
        commands = ['rm /etc/fstab', 'rm /boot/grub/grub.cfg']
        commands += [f'write {local} {path}' for path,local in paths.items()]
        commands += [
            'symlink /etc/systemd/system/systemd-networkd-wait-online.service /dev/null',
            'symlink /etc/systemd/system/multi-user.target.wants/gemini-install.service ../gemini-install.service']
        cmds=Path(tmp)/'commands'
        cmds.write_text('\n'.join(commands)+'\n')
        p=run([debugfs,'-w','-f',cmds,part],text=True,capture_output=True,check=True)
        if any(x for x in p.stderr.splitlines() if not x.startswith('debugfs ')):
            raise RuntimeError(p.stderr)
        if 'console=hvc0' not in read('cat /boot/grub/grub.cfg') or 'Prepare isolated' not in read('cat /etc/systemd/system/gemini-install.service'):
            raise RuntimeError('guest image verification failed')
        checked = run([sb/'e2fsck','-f','-p',part])
        if checked.returncode not in (0,1):
            raise RuntimeError('final filesystem check failed')
    finally:
        run(['hdiutil','detach',base],check=True)
