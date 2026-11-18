"""HTTPS CONNECT over a named virtio-serial port. No IP interface is needed."""
import asyncio
import base64
import contextlib
import json
import os
from pathlib import Path
import re
import signal
import struct
import sys

HEADER = struct.Struct('!BII')
OPEN, STATUS, DATA, ACK, CLOSE, POWER = range(1, 7)
MAX_DATA = 32768
DOMAINS = ('google.com', 'googleapis.com', 'gstatic.com', 'googleusercontent.com',
           'antigravity.google', 'google.dev', 'gvt1.com', 'deb.debian.org',
           'security.debian.org', 'www.cloudflare.com')

def permitted(target):
    if not re.fullmatch(r'[a-z0-9-]+(?:\.[a-z0-9-]+)+:443', target):
        return False
    name = target[:-4]
    return any(name == d or name.endswith('.'+d) for d in DOMAINS)

def write_all(fd, data):
    view = memoryview(data)
    while view:
        n = os.write(fd, view)
        if n <= 0:
            raise ConnectionError('serial closed')
        view = view[n:]

class Channel:
    def __init__(self):
        self.writer = None
        self.established = False
        self.ack = asyncio.Event()
        self.status = asyncio.Future()

class Bridge:
    def __init__(self, rx, tx, route=None):
        self.rx, self.tx, self.route = rx, tx, route
        self.outgoing = asyncio.Queue(maxsize=512)
        self.channels = {}
        self.last_id = 0

    async def frame(self, kind, idx, payload=b''):
        await self.outgoing.put(HEADER.pack(kind, idx, len(payload))+payload)

    async def send_frames(self):
        while True:
            payload = await self.outgoing.get()
            await asyncio.to_thread(write_all, self.tx, payload)

    async def close(self, idx, notify=True):
        stream = self.channels.pop(idx, None)
        if stream:
            stream.ack.set()
            if not stream.status.done():
                stream.status.set_result(b'502')
            if stream.writer:
                stream.writer.close()
            if notify:
                await self.frame(CLOSE, idx)

    async def pump(self, idx, reader, stream):
        while self.channels.get(idx) is stream:
            data = await reader.read(MAX_DATA)
            if not data:
                break
            stream.ack.clear()
            await self.frame(DATA, idx, data)
            await asyncio.wait_for(stream.ack.wait(), 60)

    async def upstream(self, idx, target, stream):
        try:
            if not permitted(target):
                await self.frame(STATUS, idx, b'403')
                return
            reader, writer = await asyncio.wait_for(asyncio.open_connection('127.0.0.1', self.route['mixed-port']), 8)
            if self.channels.get(idx) is not stream:
                writer.close()
                return
            stream.writer = writer
            auth = base64.b64encode(self.route['authentication'][0].encode()).decode()
            writer.write(f'CONNECT {target} HTTP/1.1\r\nHost: {target}\r\nProxy-Authorization: Basic {auth}\r\n\r\n'.encode())
            await writer.drain()
            head = await asyncio.wait_for(reader.readuntil(b'\r\n\r\n'), 30)
            if head.split(b'\r\n',1)[0].split()[1] != b'200':
                raise ConnectionError('upstream declined')
            stream.established = True
            await self.frame(STATUS, idx, b'200')
            await self.pump(idx, reader, stream)
        except (IndexError, OSError, ValueError, asyncio.TimeoutError, asyncio.IncompleteReadError, asyncio.LimitOverrunError):
            if self.channels.get(idx) is stream:
                await self.frame(STATUS, idx, b'502')
        finally:
            await self.close(idx)

    async def client(self, reader, writer):
        self.last_id = self.last_id % 0xfffffffe + 1
        idx = self.last_id
        stream = Channel()
        try:
            if len(self.channels) >= 96:
                writer.write(b'HTTP/1.1 503 Busy\r\n\r\n')
                return
            head = await asyncio.wait_for(reader.readuntil(b'\r\n\r\n'), 15)
            first = head.split(b'\r\n',1)[0].split()
            if len(first) != 3 or first[0] != b'CONNECT' or len(first[1]) > 255:
                writer.write(b'HTTP/1.1 403 Forbidden\r\n\r\n')
                return
            self.channels[idx] = stream
            # Host TLS data can only arrive after the client receives HTTP 200.
            stream.writer = writer
            await self.frame(OPEN, idx, first[1].lower())
            status = await asyncio.wait_for(stream.status, 35)
            if not writer.is_closing():
                writer.write(b'HTTP/1.1 '+status+b' '+(b'Connection established' if status == b'200' else b'Proxy refused')+b'\r\n\r\n')
                await writer.drain()
            if status == b'200':
                await self.pump(idx, reader, stream)
        except (OSError, ValueError, asyncio.TimeoutError, asyncio.IncompleteReadError, asyncio.LimitOverrunError):
            pass
        finally:
            await self.close(idx)
            writer.close()

    async def dispatch(self, kind, idx, data):
        stream = self.channels.get(idx)
        if kind == OPEN and self.route and not stream and idx:
            if len(self.channels) >= 96 or len(data) > 255:
                await self.frame(STATUS, idx, b'503')
                return
            stream = self.channels[idx] = Channel()
            asyncio.create_task(self.upstream(idx, data.decode('ascii','replace'), stream))
        elif kind == POWER and self.route is None and idx == 0:
            await asyncio.create_subprocess_exec('/usr/bin/systemctl','--no-block','poweroff')
        elif stream and kind == STATUS and not self.route and data in (b'200', b'403', b'502', b'503'):
            if not stream.status.done():
                stream.status.set_result(data)
        elif stream and kind == DATA and stream.writer and (self.route is None or stream.established):
            try:
                stream.writer.write(data)
                await asyncio.wait_for(stream.writer.drain(), 15)
                await self.frame(ACK, idx)
            except (OSError, asyncio.TimeoutError):
                await self.close(idx)
        elif stream and kind == ACK:
            stream.ack.set()
        elif stream and kind == CLOSE:
            # On a refused CONNECT let the local client send the HTTP status.
            if self.route or not stream.status.done() or stream.status.cancelled() or stream.status.result() == b'200':
                await self.close(idx, notify=False)

    async def run(self):
        # Both ends must keep reading even when serial output is backpressured.
        sending = asyncio.create_task(self.send_frames())
        receiving = asyncio.create_task(self.receive_frames())
        try:
            done, _ = await asyncio.wait((sending, receiving),return_when=asyncio.FIRST_COMPLETED)
            for task in done:
                task.result()
        finally:
            for task in (sending, receiving):
                task.cancel()

    async def receive_frames(self):
        buffer = bytearray()
        while True:
            chunk = await asyncio.to_thread(os.read, self.rx, 65536)
            if not chunk:
                break
            buffer.extend(chunk)
            while len(buffer) >= HEADER.size:
                kind, idx, length = HEADER.unpack_from(buffer)
                if length > MAX_DATA:
                    raise ValueError('oversized serial frame')
                end = HEADER.size + length
                if len(buffer) < end:
                    break
                data = bytes(buffer[HEADER.size:end])
                del buffer[:end]
                await self.dispatch(kind, idx, data)

async def main():
    if len(sys.argv) > 1:
        route = json.loads(Path(sys.argv[1]).read_text())
        bridge = Bridge(0, 1, route)
        asyncio.get_running_loop().add_signal_handler(signal.SIGUSR1, lambda: asyncio.create_task(bridge.frame(POWER,0)))
        await bridge.run()
    else:
        fd = os.open('/dev/virtio-ports/gemini.proxy', os.O_RDWR)
        bridge = Bridge(fd, fd)
        async with await asyncio.start_server(bridge.client, '127.0.0.1', 3128):
            await bridge.run()

if __name__ == '__main__':
    asyncio.run(main())
