"""Import only Cage's applied route into a private Gemini Cage profile."""
import json
import os
from pathlib import Path
import secrets
import tempfile
from urllib.parse import urlsplit

import yaml

os.umask(0o077)
destination = Path.home() / 'Library/Application Support/Gemini Cage'
destination.mkdir(mode=0o700, parents=True, exist_ok=True)
source = Path.home() / '.claude-cage/proxy/config.yaml'
applied = yaml.safe_load(source.read_bytes())
groups = applied['proxy-groups']
if (applied['rules'] != ['MATCH,CAGE'] or len(groups) != 1 or
        groups[0]['name'] != 'CAGE' or len(groups[0]['proxies']) != 1):
    raise ValueError('当前路由格式有变化，无法确认选中的出口')
nodes = {n['name']: n for n in applied['proxies']}
selected = groups[0]['proxies'][0]
route = []
current = selected
while current:
    if current not in nodes or any(n['name'] == current for n in route):
        raise ValueError('当前代理链不完整')
    node = nodes[current]
    if node['type'].lower() in ('direct', 'reject', 'pass'):
        raise ValueError('请选择实际代理出口')
    route.append(node)
    current = node.get('dialer-proxy')
original = json.loads((source.parent.parent/'config.json').read_text())
managed = urlsplit(original['proxy_url'])
if managed.scheme not in ('socks5','socks5h') or managed.hostname != '127.0.0.1' or managed.port != applied['mixed-port']:
    raise ValueError('无法确认 Claude Cage 当前使用已选中的本地 SOCKS5 代理')
secret = secrets.token_hex(32)
config = {k: applied[k] for k in ('dns','interface-name','unified-delay') if k in applied}
config.update({'mixed-port': 27896, 'bind-address':'127.0.0.1', 'allow-lan':False,
               'authentication':['gemini:'+secret], 'skip-auth-prefixes':[],
               'mode':'rule','ipv6':False,'log-level':'warning','find-process-mode':'off',
               'geo-auto-update':False,'profile':{'store-selected':False},
               'tun':{'enable':False},'proxies':route, 'proxy-groups':groups,
               'rules':['MATCH,CAGE']})
with tempfile.NamedTemporaryFile(dir=destination, delete=False, mode='w') as out:
    json.dump(config, out)
os.replace(out.name, destination/'route.json')
