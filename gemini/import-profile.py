"""Initialize a new local workbench profile from the selected route only."""
import json
import os
from pathlib import Path
import secrets
import tempfile
import shutil

def initialize():
    os.umask(0o077)
    home = Path.home()
    previous = home/'Library/Application Support/Gemini Cage/route.json'
    source = home/'.claude-cage'
    target = home/'Library/GeminiCage'
    if target.exists() or target.is_symlink():
        raise FileExistsError('已有 Gemini Cage 工作台配置，已保留')
    cfg = json.loads((source/'config.json').read_bytes())
    original = json.loads((source/'proxy/sources.json').read_bytes())
    old = json.loads(previous.read_bytes())
    selected = old['proxy-groups'][0]['proxies'][0]
    assert original['selected'] == selected, '当前出口已变更，请重新确认所选节点'
    nodes = {n['id']:n for group in original['sources'] for n in group['nodes']}
    route = [n['name'] for n in old['proxies']]
    if selected not in nodes or selected != route[0] or original['revision'] != cfg['proxy_revision']:
        raise ValueError('代理尚未应用或选择不一致')
    chain = original.get('chain') or list(reversed(route))
    if list(reversed(chain)) != route:
        raise ValueError('两个环境的代理链路不一致')
    for n in old['proxies']:
        current = nodes[n['name']]['config']
        if any(current.get(k) != v for k,v in n.items() if k != 'dialer-proxy'):
            raise ValueError('节点设置已变更')
    auth = secrets.token_hex(24)
    rev = secrets.token_hex(24)
    store = dict(version=1, port=27898, auth=auth, revision=rev,selected=selected,chain=chain,
        sources=[dict(id=secrets.token_hex(8),name='当前出口',interval_hours=0,nodes=[nodes[n] for n in chain])],
        exit_policies={selected:cfg.get('exit_policy',{})})
    config = {k:cfg[k] for k in ['dns_url','timezone','locale','browser_language','exit_policy'] if k in cfg}
    config.update(proxy_url=f'socks5://cage:{auth}@127.0.0.1:27898',proxy_revision=rev)
    original_prefs = json.loads((source/'workbench.json').read_bytes())
    prefs = {k:original_prefs[k] for k in ['balanced_display','project_file_cache','keyboard','clock_12_hour','auto_timezone','auto_language'] if k in original_prefs}
    prefs['browser_home_url']='https://gemini.google.com/'
    with tempfile.TemporaryDirectory(dir=target.parent,prefix='.gemini-workbench-import-') as stage:
        profile=Path(stage)/'profile';(profile/'proxy').mkdir(parents=True)
        for path,data in [('config.json',config),('proxy/sources.json',store),('workbench.json',prefs)]:
            (profile/path).write_text(json.dumps(data,indent=2))
        if target.exists() or target.is_symlink():raise FileExistsError('目标已经存在')
        profile.rename(target)

if __name__=='__main__':
    initialize()
