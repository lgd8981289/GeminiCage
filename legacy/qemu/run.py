"""Run the private proxy, guest display and serial bridge as one local session."""
import asyncio
import contextlib
import fcntl
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time

# Load only the relay bundled alongside this program (Python runs in isolated mode).
sys.path.insert(0, str(Path(__file__).resolve().parent))
from relay import Bridge
from power import poweroff

ROOT = Path.home() / 'Library/Application Support/Gemini Cage'
QEMU = '/opt/homebrew/bin/qemu-system-aarch64'
CORE = '/Applications/Cage.app/Contents/Resources/mihomo'

async def stop(proc):
    if proc and proc.returncode is None:
        proc.terminate()
        try:
            await asyncio.wait_for(proc.wait(), 8)
        except asyncio.TimeoutError:
            proc.kill()
            await proc.wait()

async def owns_listener(proc, port):
    args = ['/usr/sbin/lsof','-nP','-a','-p',str(proc.pid),'-iTCP:'+str(port),'-sTCP:LISTEN','-Fn']
    for _ in range(30):
        if proc.returncode is not None:
            return False
        check = await asyncio.create_subprocess_exec(*args,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        info,_ = await check.communicate()
        if ('n127.0.0.1:'+str(port)).encode() in info.splitlines():
            return True
        await asyncio.sleep(.12)
    return False

async def main():
    os.umask(0o077)
    lock = open(ROOT/'vm.lock','a')
    try:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    except BlockingIOError:
        return
    cfg = json.loads((ROOT/'route.json').read_text())
    if cfg['mixed-port'] != 27896 or cfg['rules'] != ['MATCH,CAGE']:
        raise RuntimeError('独立代理配置无效')
    for name in ['debian.raw','seed.iso']:
        if not (ROOT/name).is_file():
            raise RuntimeError('找不到虚拟机文件 '+name)
    if not Path(QEMU).is_file() or not Path(CORE).is_file():
        raise RuntimeError('找不到 QEMU 或 Cage 内附的 Mihomo 运行程序')
    path = ROOT/'proxy.sock'
    if path.exists():
        path.unlink()
    core = vm = conn = None
    try:
        core = await asyncio.create_subprocess_exec(CORE,'-d',str(ROOT/'core'),'-f',str(ROOT/'route.json'),
                        stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        if not await owns_listener(core, cfg['mixed-port']):
            raise RuntimeError('Gemini Cage 代理端口 27896 不可用')
        args = [QEMU, '-name','Gemini Cage - Antigravity','-machine','virt,accel=hvf',
             '-cpu','host','-smp','4','-m','4096', '-nodefaults','-nic','none',
             '-bios','/opt/homebrew/share/qemu/edk2-aarch64-code.fd',
             '-drive','if=none,id=disk,format=raw,file='+str(ROOT/'debian.raw'),
             '-device','virtio-blk-pci,drive=disk,bootindex=0',
             '-drive','if=none,id=seed,format=raw,readonly=on,file='+str(ROOT/'seed.iso'),
             '-device','virtio-blk-pci,drive=seed',
             '-device','virtio-gpu-pci,xres=1280,yres=800','-device','qemu-xhci',
             '-device','usb-kbd','-device','usb-tablet','-device','virtio-rng-pci',
             '-device','virtio-serial-pci','-chardev','file,id=log,append=on,path='+str(ROOT/'boot.log'),
             '-device','virtconsole,chardev=log','-chardev','socket,id=proxy,server=on,wait=off,path='+str(path),
             '-device','virtserialport,chardev=proxy,name=gemini.proxy','-display','cocoa','-qmp','unix:'+str(ROOT/'control.sock')+',server=on,wait=off','-serial','none']
        with (ROOT/'vm.log').open('ab') as log:
            vm = await asyncio.create_subprocess_exec(*args,stdin=subprocess.DEVNULL,stdout=log,stderr=log)
        conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        for _ in range(90):
            if vm.returncode is not None:
                raise RuntimeError('虚拟机启动失败，请查看 vm.log')
            try:
                conn.connect(str(path))
                break
            except (FileNotFoundError,ConnectionRefusedError):
                await asyncio.sleep(.2)
        else:
            raise RuntimeError('虚拟机串行通道未就绪')
        bridge = Bridge(conn.fileno(),conn.fileno(),cfg)
        async def shutdown():
            await asyncio.to_thread(poweroff,ROOT)
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGUSR1,signal.SIGTERM):
            loop.add_signal_handler(sig,lambda:asyncio.create_task(shutdown()))
        bridge_task = asyncio.create_task(bridge.run())
        vm_task = asyncio.create_task(vm.wait())
        core_task = asyncio.create_task(core.wait())
        done,_ = await asyncio.wait([bridge_task, vm_task, core_task],return_when=asyncio.FIRST_COMPLETED)
        if vm_task not in done:
            # A lost proxy or serial link immediately closes active tunnels.
            if conn:
                with contextlib.suppress(OSError): conn.shutdown(socket.SHUT_RDWR)
            if bridge_task in done:
                with contextlib.suppress(OSError): bridge_task.result()
            await vm.wait()
    finally:
        if conn:
            with contextlib.suppress(OSError): conn.shutdown(socket.SHUT_RDWR)
            conn.close()
        await stop(vm)
        await stop(core)
        path.unlink(missing_ok=True)

if __name__ == '__main__':
    try:
        asyncio.run(main())
    except Exception as e:
        with (ROOT/'launcher.log').open('a') as log:
            print(time.strftime('%F %T'), str(e),file=log)
        subprocess.run(['/usr/bin/osascript','-e','on run argv','-e','display alert "Gemini Cage" message (item 1 of argv)','-e','end run',str(e)])
