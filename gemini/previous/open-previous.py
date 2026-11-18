"""Reopen the previous guest without changing its disk or starting a duplicate."""
import json
from pathlib import Path
import os
import socket
import subprocess
import sys

root = Path.home() / 'Library/Application Support/Gemini Cage'
if not (root/'debian.raw').is_file():
    subprocess.run(['/usr/bin/osascript','-e','display alert "尚未找到旧版 Gemini Cage 磁盘"'])
    sys.exit(1)
with socket.socket(socket.AF_UNIX) as conn:
    try:
        conn.settimeout(2)
        conn.connect(str(root/'control.sock'))
        hello = json.loads(conn.makefile('rb').readline(65536))
        if 'QMP' not in hello:
            raise ValueError('Unexpected QMP greeting')
        # Activate only the process that owns this guest's socket.
        rows = subprocess.run(['/usr/sbin/lsof','-t',str(root/'control.sock')],capture_output=True,text=True).stdout.split()
        for pid in rows:
            cmd = subprocess.run(['/bin/ps','-p',pid,'-o','comm='],capture_output=True,text=True).stdout.strip()
            if Path(cmd).name == 'qemu-system-aarch64':
                subprocess.run(['/usr/bin/osascript','-l','JavaScript','-e',
                    "ObjC.import('AppKit'); $.NSRunningApplication.runningApplicationWithProcessIdentifier("+str(int(pid))+").activateWithOptions(2)"])
                sys.exit(0)
    except (OSError, ValueError):
        pass
os.execv(sys.executable, [sys.executable,'-I','-B',str(Path(__file__).with_name('run.py'))])
