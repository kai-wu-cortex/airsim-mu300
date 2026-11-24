#!/usr/bin/env python3
"""Cold offline build/test against a verified dependency vault; no device access."""
from pathlib import Path
import argparse
import subprocess, tempfile, shutil, os, json
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--vault', required=True, type=Path)
args=parser.parse_args()
vault=str(args.vault.resolve())
r=Path(__file__).resolve().parents[1]
(r/'out').mkdir(exist_ok=True)
stage=Path(tempfile.mkdtemp(prefix='offline-check.',dir=r/'out')); project=stage/'project'; project.mkdir()
files=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=r).decode().split('\0')
for name in sorted(set(files)-{''}):
 src=r/name
 if not src.is_file() and not src.is_symlink(): continue
 dst=project/name;dst.parent.mkdir(parents=True,exist_ok=True)
 if src.is_symlink(): dst.symlink_to(os.readlink(src))
 else: shutil.copy2(src,dst)
mock=stage/'network-denied'; mock.mkdir(); events=stage/'network-attempts'
for tool in ['git','curl','wget']:
 original=shutil.which(tool)
 wrapper=mock/tool
 wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\nfrom pathlib import Path\n'+
  ('if any(x in sys.argv[1:] for x in ["clone","fetch","pull","push","ls-remote"]):\n' if tool=='git' else 'if True:\n')+
  ' Path(os.environ["NETWORK_EVENTS"]).write_text("network attempted")\n sys.exit(99)\n'+
  'os.execv('+repr(original or '/usr/bin/false')+',sys.argv)\n')
 wrapper.chmod(0o755)
env=dict(os.environ,PATH=str(mock)+':'+os.environ['PATH'],AIRSIM_OFFLINE='1',GOPROXY='off',GOSUMDB='off',GOTOOLCHAIN='local',GOWORK='off',GOMODCACHE=str(stage/'empty-modcache'),GOCACHE=str(stage/'empty-buildcache'),NETWORK_EVENTS=str(events))
env.pop('AIRSIM_UPSTREAM_REPO',None)
results=[]
def run(args,cwd=project):
 with (stage/'verification.log').open('a') as out:
  out.write('RUN '+repr(args)+'\n');out.flush()
  subprocess.run(args,cwd=cwd,env=env,stdout=out,stderr=subprocess.STDOUT,check=True)
 results.append(args)
run(['python3','scripts/dependency-vault.py','restore',vault])
for script in ['fetch-deps.sh','fetch-sources.sh','fetch-firmware.sh','build.sh']:
 run(['sh','scripts/'+script])
run(['python3','scripts/fetch-kernel-updater-fixtures.py'])
run(['python3','scripts/preserved-sources.py','plugin-vendor',str(project),'--module',str(project)])
run(['go','test','-race','./...']);run(['go','vet','./...'])
for script in ['preservation.test.py','fetch-firmware.test.py','complete-install.test.py']:
 run(['python3','scripts/'+script])
run(['node','--test',*sorted(str(p.relative_to(project)) for p in (project/'scripts').glob('*.test.mjs'))])
agent=Path(subprocess.check_output(['sh','scripts/prepare-agent.sh'],cwd=project,env=env,text=True).strip())/'module/module-agent'
run(['go','test','-race','./...'],agent);run(['go','vet','./...'],agent)
run(['sh','scripts/prepare-audio-offline.sh',vault])
assert not events.exists(),'network attempted'
result={'passed':True,'network_attempts':0,'fresh_module_and_build_cache':True,'upstream_checkout_used':False,'checks':results,'stage':str(stage),'full_apk_packaging_tested':False,'kernel_compilation_tested':False}
(stage/'result.json').write_text(json.dumps(result,indent=2)+'\n');(r/'out/preservation-verification-path').write_text(str(stage)+'\n');print(json.dumps(result,indent=2))
