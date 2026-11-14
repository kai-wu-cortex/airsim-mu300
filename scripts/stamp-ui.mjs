// Content-address the staged LuCI module graph, not the source working tree.
// LuCI's firmware resource version does not change on a plugin-only upgrade.
import {createHash} from 'node:crypto';
import {readFileSync, writeFileSync, readdirSync, renameSync} from 'node:fs';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

export function stampUI(stage) {
 const web=join(stage,'www/luci-static/resources/view/airsim');
 const files=readdirSync(web).filter(n=>/\.(js|css)$/.test(n)).sort();
 if(files.some(n=>/_[a-f0-9]{16}\.(js|css)$/.test(n)))throw Error('UI already stamped');
 const sources=new Map(files.map(n=>[n,readFileSync(join(web,n),'utf8')]));
 const hash=createHash('sha256');
 for(const [name,source] of sources)hash.update(name+'\0'+source+'\0');
 const revision=hash.digest('hex').slice(0,16);
 const renamed=n=>n.replace(/\.(js|css)$/,`_${revision}.$1`);
 const menuPath=join(stage,'usr/share/luci/menu.d/luci-app-airsim.json');
 const menu=JSON.parse(readFileSync(menuPath,'utf8'));
 const dashboardPath=join(stage,'usr/lib/airsim/dashboard-menu.json');
 const dashboard=JSON.parse(readFileSync(dashboardPath,'utf8'));
 for(const entry of Object.values(menu).concat(Object.values(dashboard))){
  if(entry.action?.type!=='view'||!entry.action.path.startsWith('airsim/'))continue;
  const name=entry.action.path.slice(7)+'.js';
  if(!sources.has(name))throw Error('Missing view: '+name);
  entry.action.path='airsim/'+renamed(name).slice(0,-3);
 }
 const transformed=new Map();
 for(const [name,source] of sources){
  const next=source.replace(/require view\.airsim\.([a-z_]+) as /g,(_,dep)=>{
   if(!sources.has(dep+'.js'))throw Error('Missing dependency: '+dep);
   return `require view.airsim.${dep}_${revision} as `;
  }).replace(/view\/airsim\/([a-z_]+\.(?:css|js))/g,(_,dep)=>{
   if(!sources.has(dep))throw Error('Missing resource: '+dep);
   return 'view/airsim/'+renamed(dep);
  });
  transformed.set(name,next);
 }
 for(const [name,source] of transformed){writeFileSync(join(web,name),source);renameSync(join(web,name),join(web,renamed(name)));}
 writeFileSync(menuPath,JSON.stringify(menu,null,2)+'\n');
 writeFileSync(dashboardPath,JSON.stringify(dashboard,null,2)+'\n');
 return revision;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 if(!process.argv[2])throw Error('Usage: node scripts/stamp-ui.mjs STAGING_ROOT');
 console.log('LuCI UI revision: '+stampUI(resolve(process.argv[2])));
}
