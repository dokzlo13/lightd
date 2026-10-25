#!/usr/bin/env python3
"""Offline, explicit V1 resource-state migration. Stop lightd and back up first.

Mapping JSON: {"groups":{"1":"zone-uuid"},"lights":{"2":"light-uuid"}}.
Only the optional named scene KV bucket is remapped; other user KV stays opaque.
"""
import argparse,json,pathlib,sqlite3,sys,uuid


def convert(payload):
    value=json.loads(payload)
    if not isinstance(value,dict):raise ValueError('desired state must be an object')
    if any(value.get(k) is not None for k in ('hue','sat')):
        raise ValueError('legacy HS desired state needs explicit XY/HSV migration before this tool can proceed')
    if value.get('alert') or value.get('effect'):
        raise ValueError('legacy alert/effect needs an explicit supported V2 choice')
    if 'bri' in value:
        if 'brightness' in value:raise ValueError('both legacy and native brightness present')
        value['brightness']=float(value.pop('bri'))*100.0/254.0
    if 'transitiontime' in value:
        if 'duration' in value:raise ValueError('both legacy and native transition present')
        value['duration']=int(value.pop('transitiontime'))*100
    for key in ('hue','sat','alert'):value.pop(key,None)
    return json.dumps(value,separators=(',',':'))


def migrate(db,mapping,scene_bucket=None):
    changes=[]
    for kind,table in [('group',mapping.get('groups',{})),('light',mapping.get('lights',{}))]:
        for old,new in sorted(table.items()):
            uuid.UUID(new)
            row=db.execute('SELECT payload FROM resource_state WHERE kind=? AND id=?',(kind,str(old))).fetchone()
            if not row:continue
            if str(old)==new:continue
            if db.execute('SELECT 1 FROM resource_state WHERE kind=? AND id=?',(kind,new)).fetchone():raise ValueError('native desired target already exists; resolve collision before migrating')
            payload=convert(row[0])
            db.execute('UPDATE resource_state SET id=?,payload=?,version=version+1 WHERE kind=? AND id=?',(new,payload,kind,str(old)))
            changes.append(kind)
    if scene_bucket:
        for old,new in sorted(mapping.get('groups',{}).items()):
            if str(old)==new:continue
            row=db.execute('SELECT 1 FROM kv_store WHERE bucket=? AND key=?',(scene_bucket,str(old))).fetchone()
            if not row:continue
            if db.execute('SELECT 1 FROM kv_store WHERE bucket=? AND key=?',(scene_bucket,new)).fetchone():raise ValueError('native scene KV key already exists; resolve collision before migrating')
            db.execute('UPDATE kv_store SET key=? WHERE bucket=? AND key=?',(new,scene_bucket,str(old)))
            changes.append('scene_kv')
    for table in (('hue_manual_fences_v1','hue_delivery_v1','hue_target_authority_v1') if changes else ()):
        if db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?",(table,)).fetchone():db.execute('DELETE FROM '+table)
    # Unknown/unmapped IDs are errors, never guesses or silently discarded rows.
    for kind,id in db.execute("SELECT kind,id FROM resource_state WHERE kind IN ('group','light')"):
        try:uuid.UUID(id)
        except ValueError:raise ValueError('unmapped legacy '+kind+' desired ID remains; complete the mapping')
    return {k:changes.count(k) for k in sorted(set(changes))}


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--database',required=True,type=pathlib.Path)
    p.add_argument('--mapping',required=True,type=pathlib.Path)
    p.add_argument('--scene-bucket',help='Optional exact script-owned KV bucket whose group keys should migrate')
    p.add_argument('--apply',action='store_true',help='Commit changes; default is transaction rollback/dry-run')
    a=p.parse_args()
    if not a.database.is_file():p.error('database does not exist')
    mapping=json.loads(a.mapping.read_text())
    db=sqlite3.connect(str(a.database));db.execute('PRAGMA busy_timeout=5000')
    try:
        db.execute('BEGIN IMMEDIATE')
        summary=migrate(db,mapping,a.scene_bucket)
        if a.apply:db.commit()
        else:db.rollback()
        print(json.dumps({'mode':'applied' if a.apply else 'dry-run','changed':summary}))
    finally:db.close()
if __name__=='__main__':
    try:main()
    except (ValueError,sqlite3.Error) as e:raise SystemExit('Migration refused: '+str(e))
