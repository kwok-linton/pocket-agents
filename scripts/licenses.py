#!/usr/bin/env python3
"""Collect local locked dependency license texts for a release bundle."""
import json,pathlib,re,subprocess
root=pathlib.Path(__file__).resolve().parents[1];out=root/'bin/licenses';out.mkdir(parents=True,exist_ok=True)
data=subprocess.check_output(['go','list','-m','-json','all'],cwd=root,text=True);decoder=json.JSONDecoder();pos=0;mods=[]
while pos<len(data):
 while pos<len(data) and data[pos].isspace():pos+=1
 if pos==len(data):break
 item,n=decoder.raw_decode(data,pos);mods.append(item);pos=n
for m in mods:
 if m.get('Main') or not m.get('Dir'):continue
 d=pathlib.Path(m['Dir']);target=out/re.sub('[^a-zA-Z0-9._-]','_',m['Path']+'@'+m.get('Version',''));target.mkdir(exist_ok=True)
 for p in d.iterdir():
  if p.is_file() and p.name.lower().startswith(('license','copying','notice')):target.joinpath(p.name).write_bytes(p.read_bytes())
for package in (root/'web/node_modules').glob('**/package.json'):
 if package.parent.name in ('test','fixtures'):continue
 try:m=json.loads(package.read_text())
 except Exception:continue
 if not m.get('name'):continue
 target=out/re.sub('[^a-zA-Z0-9._-]','_',m['name']+'@'+m.get('version',''));target.mkdir(exist_ok=True)
 for p in package.parent.iterdir():
  if p.is_file() and p.name.lower().startswith(('license','copying','notice')):target.joinpath(p.name).write_bytes(p.read_bytes())
print('Collected locked dependency notices:',out)
