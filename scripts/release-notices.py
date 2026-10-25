#!/usr/bin/env python3
"""Collect licenses for packages compiled into lightd; no private runtime inputs."""
import argparse, json, pathlib, re, shutil, subprocess

parser=argparse.ArgumentParser()
parser.add_argument('output',type=pathlib.Path)
args=parser.parse_args()
args.output.mkdir(parents=True,exist_ok=True)
raw=subprocess.check_output(['go','list','-deps','-json','./cmd/lightd'],text=True)
decoder=json.JSONDecoder(); modules={}
while raw.strip():
    obj,n=decoder.raw_decode(raw.lstrip());raw=raw.lstrip()[n:]
    mod=obj.get('Module',{})
    if mod and not mod.get('Main'):modules[mod['Path']]=mod
inventory=[]
for name,mod in sorted(modules.items()):
    directory=pathlib.Path(mod['Dir']);dest=args.output/name.replace('/','_');dest.mkdir(parents=True,exist_ok=True)
    files=[f for f in directory.iterdir() if f.is_file() and (f.name.upper().startswith(('LICENSE','COPYING','NOTICE')))]
    if not files:raise SystemExit('Missing license for '+name)
    for file in files:shutil.copyfile(file,dest/file.name)
    entry={'module':name,'version':mod.get('Version'),'licenses':[str((dest/f.name).relative_to(args.output)) for f in files]}
    if name=='github.com/mattn/go-sqlite3':
        source=directory/'sqlite3-binding.c'
        if source.exists():
            found=re.search(r'^#define SQLITE_VERSION\s+"([^"]+)"',source.read_text(),re.M)
            if found:entry['bundled_sqlite_version']=found.group(1)
    inventory.append(entry)
goroot=pathlib.Path(subprocess.check_output(['go','env','GOROOT'],text=True).strip())
shutil.copyfile(goroot/'LICENSE',args.output/'GO-LICENSE')
(args.output/'dependencies.json').write_text(json.dumps(inventory,indent=2)+'\n')
print('Collected licenses for',len(inventory),'compiled dependency modules and Go')
