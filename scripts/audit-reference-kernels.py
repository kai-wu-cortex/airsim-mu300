#!/usr/bin/env python3
"""Inventory an isolated reference checkout. Missing CRC records are evidence
limitations, not proof of incompatibility: CONFIG_MODVERSIONS may be disabled.
This report never marks a kernel/install profile as verified."""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import subprocess

def inspect(path):
    data = path.read_bytes()
    if data[:6] != b'\x7fELF\x02\x01' or struct.unpack_from('<H',data,18)[0] != 183:
        raise ValueError('expected AArch64 ELF64 LE')
    offset = struct.unpack_from('<Q',data,40)[0]
    size,count,names_index = struct.unpack_from('<HHH',data,58)
    if size != 64 or count == 0 or names_index >= count or offset+size*count > len(data):
        raise ValueError('invalid section table')
    headers = [struct.unpack_from('<IIQQQQIIQQ',data,offset+i*size) for i in range(count)]
    h = headers[names_index]
    names = data[h[4]:h[4]+h[5]]
    sections = {}
    for h in headers:
        if h[1] == 8: continue
        if h[4]+h[5] > len(data): raise ValueError('section outside file')
        name = names[h[0]:].split(b'\0',1)[0].decode('ascii')
        sections[name] = data[h[4]:h[4]+h[5]]
    magic = [v[9:].decode() for v in sections.get('.modinfo',b'').split(b'\0') if v.startswith(b'vermagic=')]
    if len(magic)!=1: raise ValueError('ambiguous/missing vermagic')
    return {'name':path.name,'sha256':hashlib.sha256(data).hexdigest(),'vermagic':magic[0],'crc_section_present':bool(sections.get('__versions')),'extended_crc_section_present':bool(sections.get('__version_ext_crcs'))}

def audit(reference):
    pin = subprocess.run(['git','-C',str(reference),'rev-parse','HEAD'],capture_output=True,text=True,check=True).stdout.strip()
    reports=[]
    for directory in sorted((reference/'parts/audio/payload').iterdir()):
        if not directory.is_dir(): continue
        meta=dict(line.split('=',1) for line in (directory/'KERNEL').read_text().splitlines() if '=' in line)
        modules=[inspect(p) for p in sorted(directory.glob('*.ko'))]
        reports.append({'kernel':meta.get('KREL'),'reference_config_hash':meta.get('CONFIG'),'module_count':len(modules),'release_metadata_match':bool(modules) and all(m['vermagic'].split()[0]==meta.get('KREL') for m in modules),'modules':modules,'abi_verified':False,'hardware_verified':False,'blocking_evidence':['Exact build config and normalization provenance must be established','Kernel/peer symbol and layout compatibility must be audited with the matching source/build','No installation bypass of config/vermagic/symbol checks is accepted']})
    return {'reference_commit':pin,'profiles':reports,'note':'Inventory only. Missing legacy __versions alone does not prove an ABI defect.'}
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('reference',type=Path);p.add_argument('--report',type=Path,required=True);a=p.parse_args();r=audit(a.reference);a.report.parent.mkdir(parents=True,exist_ok=True);a.report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'reference_commit':r['reference_commit'],'profiles':[{'kernel':x['kernel'],'modules':x['module_count'],'release_metadata_match':x['release_metadata_match'],'abi_verified':x['abi_verified']} for x in r['profiles']]},indent=2))
