"""Ask just this VM's guest OS to shut down through its private QMP socket."""
import json
import socket
from pathlib import Path

def poweroff(root):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as conn:
        conn.settimeout(5)
        conn.connect(str(root/'control.sock'))
        with conn.makefile('rwb',buffering=0) as control:
            if 'QMP' not in json.loads(control.readline()):
                raise RuntimeError('虚拟机控制通道无效')
            for name in ('qmp_capabilities','system_powerdown'):
                control.write((json.dumps({'execute':name})+'\n').encode())
                while True:
                    reply = json.loads(control.readline())
                    if 'return' in reply:
                        break
                    if 'error' in reply:
                        raise RuntimeError('虚拟机关机请求失败')

if __name__ == '__main__':
    try:
        poweroff(Path.home()/'Library/Application Support/Gemini Cage')
        print('已请求客机正常关机，窗口会在保存后关闭。')
    except (FileNotFoundError, ConnectionRefusedError):
        print('Gemini Cage 已关闭。')
