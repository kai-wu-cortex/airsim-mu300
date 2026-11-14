'use strict';
'require baseclass';
'require view.airsim.ui as ui';
'require poll';
'require view.airsim.device as device';
'require view.airsim.appearance as appearance';
return baseclass.extend({
 render:function(page){
  var feedback=E('p',{'role':'status','aria-live':'polite','class':'as-feedback'}), busy=false, jobID='', completion=null;
  var controls=[];
  function lock(value){busy=value;controls.forEach(function(b){b.disabled=value||!!b.unavailable;});}
  function message(s){feedback.textContent=s;}
  async function result(r){
   if(r.pending){jobID=r.id;message(r.message);return;}
   if(r.error||!r.ok)throw Error(r.error||'结果未确认');
   message(r.message||'操作已完成。');if(completion)await completion(r);completion=null;lock(false);
  }
  async function submit(action,fields,question,done){
   if(busy||!window.confirm(question+'\n请先结束通话，并在概览关闭 AirSIM 服务。'))return;
   lock(true);completion=done||null;
   try{await result(await device.write(action,fields));}catch(e){message(e.message+' 不会自动重试。请刷新维护状态。');completion=null;lock(false);}
  }
  function action(label,fn){var b=device.action(label,fn);controls.push(b);return b;}
  function setAvailable(b,yes){b.unavailable=!yes;b.disabled=busy||!yes;}
  async function checkJob(){
   try{var r=await device.read('maintenance_job');if(!r.job){return;}var j=r.job;
    if(!jobID){if(!j.done){jobID=j.id;lock(true);}else{return;}}
    if(j.id!==jobID){message('维护记录已变化，请刷新页面核对。');return;}
    if(r.uncertain){message('维护结果暂未确认。请检查设备，不要重复提交或重启。');return;}
    if(j.done){jobID='';try{await result(j.result);}catch(e){message(e.message);completion=null;lock(false);}}
   }catch(e){if(busy)message('连接中断，维护任务可能仍在继续。恢复连接后会查询结果，不会重发。');}
  }
  poll.add(checkJob,3);checkJob();
  var networkInfo=E('p',{'class':'as-copy','role':'status','aria-live':'polite'},'正在读取联网状态…'), networkBadge=E('span',{'class':'as-pill as-pill-neutral'},'读取中'), networks=E('div');
  var mode=device.select([['cell','蜂窝联网'],['wifi','Wi-Fi 联网'],['ap','蜂窝联网＋热点']]);
  var ssid=device.input('text','Wi-Fi 名称'),password=device.input('password','Wi-Fi 密码');
  var encryption=device.select([['psk2','WPA2'],['sae','WPA3'],['none','开放网络']]);
  var switchMode=action('切换联网模式',function(){return submit('network_mode',{mode:mode.value},'切换会短暂断网，USB 管理配置会保留。',refreshNetwork);});
  var connect=action('连接 Wi-Fi',function(){return submit('wifi_connect',{ssid:ssid.value,password:password.value,encryption:encryption.value},'连接所填 Wi-Fi，失败将尝试恢复原网络。',function(){password.value='';return refreshNetwork();});});
  var forget=action('忘记 Wi-Fi',function(){return submit('wifi_forget',{},'删除已保存的 Wi-Fi 名称和密码，并切回蜂窝联网。',refreshNetwork);});
  var wifiAdmin=action('允许 Wi-Fi 访问管理页',function(){return submit('wifi_admin',{enabled:true},'允许当前 Wi-Fi 网络访问本机网页管理端口（仍需登录）。');});
  var wifiClose=action('关闭新增 Wi-Fi 管理规则',function(){return submit('wifi_admin',{enabled:false},'关闭本功能添加的 Wi-Fi 管理规则；不删除其他管理员已有规则。');});
  var networkState=null,savedNetwork=E('p'),currentMode=E('strong'),adminState=E('p',{'class':'as-caption'},'正在读取…');
  var networkToggle=E('input',{type:'checkbox',change:function(){var next=networkToggle.checked;networkToggle.checked=!!(networkState&&networkState.mode==='wifi');if(!networkToggle.disabled)return submit('network_mode',{mode:next?'wifi':'ap'},'切换联网模式会短暂断网。',refreshNetwork);}});
  var adminToggle=E('input',{type:'checkbox',change:function(){var next=adminToggle.checked;adminToggle.checked=!!(networkState&&networkState.admin_enabled);if(!adminToggle.disabled)return submit('wifi_admin',{enabled:next},next?'允许当前 Wi-Fi 网络访问管理页（仍需登录）。':'关闭本功能的 Wi-Fi 管理规则。',refreshNetwork);}});
  controls.push(networkToggle,adminToggle);setAvailable(networkToggle,false);setAvailable(adminToggle,false);
  function paintNetwork(r){
   networkState=r;networkToggle.checked=!!(r&&r.mode==='wifi');adminToggle.checked=!!(r&&r.admin_enabled);
   setAvailable(networkToggle,!!(r&&r.supported&&['cell','wifi','ap'].includes(r.mode)));setAvailable(adminToggle,!!(r&&r.supported&&r.admin_known));
   currentMode.textContent=r?({wifi:'正在用 Wi-Fi 上网',ap:'正在用流量上网，热点开着',cell:'正在用流量上网'}[r.mode]||'当前模式未知'):'当前模式未知';
   savedNetwork.textContent=r&&r.ssid?r.ssid+' · '+(r.has_password?'密码已保存':'未保存密码'):'尚未保存 Wi-Fi';
   adminState.textContent=!r||!r.admin_known?'管理访问状态未知':r.admin_enabled?(r.admin_applied?'已允许 Wi-Fi 访问管理页':'已配置，规则尚未生效'):(r.admin_applied?'配置与运行规则不一致，请检查':'本功能的 Wi-Fi 管理规则已关闭');
  }
  async function refreshNetwork(){networkInfo.setAttribute('aria-busy','true');try{var r=await device.read('network');paintNetwork(r);networkBadge.textContent=r.supported?'布局已识别':'暂不可切换';networkBadge.className='as-pill '+(r.supported?'as-pill-green':'as-pill-neutral');networkInfo.textContent=(r.supported?'接口布局已识别':'接口布局未适配：'+r.reason)+'；当前模式：'+({cell:'蜂窝',wifi:'Wi-Fi',ap:'热点',unknown:'自定义'}[r.mode]||'未知')+'；已保存 Wi-Fi：'+(r.ssid||'无');[switchMode,connect,forget,wifiAdmin,wifiClose].forEach(function(b){setAvailable(b,r.supported);});}catch(e){paintNetwork(null);networkBadge.textContent='状态未知';networkBadge.className='as-pill as-pill-neutral';networkInfo.textContent=e.message;[switchMode,connect,forget,wifiAdmin,wifiClose].forEach(function(b){setAvailable(b,false);});}finally{networkInfo.setAttribute('aria-busy','false');}}
  [switchMode,connect,forget,wifiAdmin,wifiClose].forEach(function(b){setAvailable(b,false);});
  var scan=action('扫描 Wi-Fi',function(){return submit('wifi_scan',{},'扫描会短暂使用无线电。',function(r){networks.replaceChildren();(r.results||[]).forEach(function(n){networks.appendChild(E('p',{},String(n.ssid||'隐藏网络')+' · '+String(n.signal||'')+' dBm'));});});});
  var flow=E('p');async function refreshFlow(){try{var r=await device.read('flow_status');flow.textContent=r.supported?(r.rules_readable?(r.risk?'检测到 5.4 热点流卸载风险':'未检测到目标冲突'):'无法读取防火墙规则'):'当前内核不适用 5.4 流卸载修复';}catch(e){flow.textContent=e.message;}}
  var bt=E('p'),btDevices=E('div'),btAddress=device.input('text','AA:BB:CC:DD:EE:FF');
  async function refreshBluetooth(){try{var r=await device.read('bluetooth');bt.textContent=r.supported?'蓝牙控制器：'+(r.controller.alias||r.controller.name||'已识别')+' · '+(r.controller.powered==='yes'?'已开启':'已关闭'):r.reason;btActions.forEach(function(b){setAvailable(b,r.supported);});btDevices.replaceChildren();(r.devices||[]).forEach(function(d){btDevices.appendChild(E('p',{},String(d.name)+' · '+d.address));});}catch(e){bt.textContent=e.message;btActions.forEach(function(b){setAvailable(b,false);});}}
  var btActions=[action('开启蓝牙',function(){return submit('bt_power',{enabled:true},'开启设备蓝牙并保存开机偏好。',refreshBluetooth);}),action('关闭蓝牙',function(){return submit('bt_power',{enabled:false},'关闭设备蓝牙，会断开已连接的蓝牙设备。',refreshBluetooth);}),action('扫描蓝牙',function(){return submit('bt_scan',{},'扫描附近蓝牙设备约 8 秒。',refreshBluetooth);})];
  [['bt_pair','配对'],['bt_connect','连接'],['bt_disconnect','断开'],['bt_remove','移除']].forEach(function(item){btActions.push(action(item[1],function(){return submit(item[0],{address:btAddress.value.trim()},item[1]+'指定蓝牙设备。验证码须由已有 BlueZ 配对代理确认。',refreshBluetooth);}));});
  btActions.forEach(function(b){setAvailable(b,false);});
  var backupFile=E('input',{type:'file',accept:'.json,application/json'});
  var backup=action('导出本机配置',function(){return submit('backup_export',{},'备份包含配对凭据和通知密钥，请私密保存。',function(r){var blob=new Blob([Uint8Array.from(atob(r.payload),function(c){return c.charCodeAt(0);})],{type:'application/json'});var url=URL.createObjectURL(blob),a=E('a',{href:url,download:r.filename});a.click();setTimeout(function(){URL.revokeObjectURL(url);},1000);});});
  var restore=action('恢复选中的配置',async function(){var f=backupFile.files&&backupFile.files[0];if(!f||f.size>768*1024){message('请选择不超过 768 KiB 的本项目备份。');return;}var bytes=new Uint8Array(await f.arrayBuffer()),binary='';bytes.forEach(function(b){binary+=String.fromCharCode(b);});return submit('backup_restore',{payload:btoa(binary)},'恢复会替换本机的配对和插件设置。只接受同一硬件的备份，恢复前会保存当前副本。');});
  var enabled=E('input',{type:'checkbox'}),include=E('input',{type:'checkbox'}),provider=device.select([['webhook','通用 Webhook'],['bark','Bark'],['telegram','Telegram'],['pushplus','PushPlus'],['serverchan','ServerChan'],['wecom','企业微信'],['dingtalk','钉钉']]);
  var endpoint=device.input('password','完整 HTTPS 通知地址；同渠道留空保留'),secret=device.input('password','PushPlus token；同渠道留空保留'),chat=device.input('text','Telegram chat_id'),notifyHint=E('p');
  async function loadNotifications(){try{var r=await device.read('notifications');enabled.checked=r.enabled;include.checked=r.include_number;provider.value=r.provider;chat.value=r.chat_id||'';notifyHint.textContent=r.has_url?'已保存通知地址，密钥不会回显。':'尚未配置通知地址。';}catch(e){notifyHint.textContent=e.message;}}
  var saveNotify=action('保存通知设置',function(){var c={enabled:enabled.checked,provider:provider.value,url:endpoint.value.trim(),secret:secret.value,chat_id:chat.value.trim(),include_number:include.checked};return submit('notifications_save',{payload:JSON.stringify(c)},'启用后会向所填服务发送通话事件'+(c.include_number?'及电话号码':'（不含电话号码）')+'，不转发声音。',function(){endpoint.value='';secret.value='';return loadNotifications();});});
  var checks=E('div');async function selfcheck(){try{var r=await device.read('selfcheck');checks.replaceChildren();r.checks.forEach(function(c){checks.appendChild(E('p',{},(c.passed?'✓ ':'待检查 · ')+c.name+'：'+c.detail));});}catch(e){checks.textContent=e.message;}}
  var catalog=E('div');async function audio(){try{var r=await device.read('audio_catalog');catalog.replaceChildren();r.profiles.forEach(function(p){var install=action('准备 '+p.kernel+' 音频',function(){return submit('audio_install',{mode:p.id},'准备匹配的音频扩展；如提示需要重启，请另外安排。不会更换内核。',audio);});setAvailable(install,p.installable);catalog.appendChild(E('div',{},[E('p',{},p.kernel+'：'+p.evidence),E('p',{},p.installable?'本机配置及本地包哈希匹配，安装时仍会执行完整预检':'当前不可安装'),install]));});}catch(e){catalog.textContent=e.message;}}
  if(!page||page==='net')refreshNetwork();if(!page||page==='maintenance')refreshFlow();if(!page||page==='bluetooth')refreshBluetooth();if(!page||page==='call')loadNotifications();if(!page||page==='audio'){selfcheck();audio();}
  if(page){
   var card=ui.card,copy=function(text){return E('p',{'class':'as-caption'},text);};
   var parts=[];
   if(page==='net')parts=[
    card('上网方式',[E('div',{'class':'as-inline'},[currentMode,networkBadge]),appearance.switchField('Wi-Fi 上网',networkToggle),networkInfo,E('details',{'class':'as-details'},[E('summary',{},'高级联网模式'),device.field('联网模式',mode),switchMode]),action('刷新网络状态',refreshNetwork),copy('开：连接外部 Wi-Fi。关：蜂窝联网并开启热点。仅蜂窝联网可在高级选项选择。失败时尝试恢复原网络。')]),
    card('连接 Wi-Fi',[copy('扫描可能短暂影响无线连接，建议通过 USB 管理。'),scan,networks,E('div',{'class':'as-details'},[copy('隐藏网络 / 手动输入'),device.field('Wi-Fi 名称',ssid),device.field('密码',password),device.field('加密方式',encryption),connect])]),
    card('已保存的 Wi-Fi',[savedNetwork,forget,copy('密码不会回显；删除前会再次确认。')]),
    card('管理页面访问',[adminState,appearance.switchField('允许从 Wi-Fi 打开',adminToggle),E('p',{'class':'as-notice'},'开启后，同一 Wi-Fi 中的设备可访问登录页面。请设置管理密码，仅在可信网络使用。'),copy('仅管理本功能的 HTTP/HTTPS 规则，不开放 SSH，不删除其他规则。')]),
    card('这台机器的限制',[copy('仅在网络布局已识别时允许切换，USB 管理配置必须保留。'),copy('无线能力以当前 U30 系统为准，不套用 F50 的固定频段与射频限制。')])
   ];
   if(page==='call')parts=[
    card('开关',[appearance.switchField('启用通知',enabled),copy('修改后点击保存并确认。仅转发通话事件，不转发声音。')]),
    card('推送渠道',[device.field('渠道',provider),copy('使用当前设备独立保存的通知渠道。')]),
    card('推送哪些状态',[copy('已接入来电、接通与结束事件。'),appearance.switchField('发送电话号码',include),copy('逐类事件开关和独立漏接筛选暂不可用。')]),
    card('请求与正文',[device.field('通知地址',endpoint),device.field('PushPlus token',secret),device.field('Telegram chat_id',chat),copy('按渠道生成结构化消息；仅支持公网 HTTPS，密钥不会回显。')]),
    card('运行状态',[notifyHint,copy('保存后由设备处理通话事件，投递失败不自动重试。')]),
    card('测试与最近投递',[saveNotify,copy('当前候选暂不提供测试投递与投递历史，保存不会发送测试消息。')])
   ];
   if(page==='bluetooth')parts=[
    card('蓝牙',[bt,E('div',{'class':'as-buttons'},btActions.slice(0,2)),action('刷新蓝牙状态',refreshBluetooth)]),
    card('本机蓝牙',[copy('复用系统已有 BlueZ 控制器；不自动安装其他内核的驱动。')]),
    card('已配对设备',[btDevices,device.field('目标地址',btAddress),E('div',{'class':'as-buttons'},btActions.slice(3))]),
    card('附近的设备',[btActions[2],copy('扫描结果更新设备列表，填写目标地址后配对。')]),
    card('说明',[copy('配对需要已有 BlueZ 代理确认。不自动开启永久可发现状态；配对成功不代表蜂窝蓝牙音频已适配。')])
   ];
   if(page==='audio')parts=[card('音频配件包',[catalog]),card('刷机后自检',[checks,action('刷新自检',selfcheck),action('修复私有配置权限',function(){return submit('selfcheck_fix',{},'仅修复本项目配置权限。',selfcheck);})])];
   if(page==='maintenance')parts=[
    card('备份',[copy('包含配对和通知密钥，请私密保存；不含短信、通话记录、音频、网络配置或固件。'),backup]),
    card('恢复',[copy('只接受同一硬件的备份，恢复前保存当前副本。'),backupFile,restore]),
    card('客户端上网体检（5.4）',[flow,action('检查流卸载',refreshFlow),action('修复 5.4 流卸载冲突',function(){return submit('flow_fix',{},'仅在检测到目标冲突时关闭软件流卸载。',refreshFlow);}),action('恢复流卸载原设置',function(){return submit('flow_restore',{},'恢复此前保存的卸载设置。',refreshFlow);})])
   ];
   return {cards:parts,feedback:feedback};
  }
  var root=E('div',{'class':'as-root'},[E('link',{rel:'stylesheet',href:L.resource('view/airsim/overview.css')}),E('h2',{},'设备扩展与维护'),E('p',{'class':'as-copy'},'修改、扫描与配置导出前，请先结束通话并在概览关闭 AirSIM 服务。任务期间不要重启或重复提交；完成后手动启动服务。只读刷新不改变设备。'),feedback,E('div',{'class':'as-management-layout'},[E('div',{'class':'as-management-stack'},[
   device.card('联网与热点',[networkBadge,networkInfo,action('刷新网络状态',refreshNetwork),device.field('联网模式',mode),switchMode,device.field('Wi-Fi 名称',ssid),device.field('加密方式',encryption),device.field('Wi-Fi 密码',password),E('div',{'class':'as-buttons'},[connect,forget,scan]),networks,E('div',{'class':'as-buttons'},[wifiAdmin,wifiClose]),flow,action('检查流卸载',refreshFlow),action('修复 5.4 流卸载冲突',function(){return submit('flow_fix',{},'仅在检测到目标冲突时关闭软件流卸载；将保存原设置。',refreshFlow);}),action('恢复流卸载原设置',function(){return submit('flow_restore',{},'恢复本功能保存的卸载设置，可能再次出现原问题。',refreshFlow);})]),
   device.card('蓝牙',[bt,action('刷新蓝牙状态',refreshBluetooth),btDevices,device.field('目标地址',btAddress),E('div',{'class':'as-buttons'},btActions),E('p',{},'只使用已安装且可工作的 BlueZ 控制器。缺少组件时不自动安装未知内核模块；配对成功不等于蜂窝蓝牙音频已适配。')]),
   device.card('本机配置备份',[E('p',{},'包含插件设置、设备身份、配对和通知密钥，不含短信、通话记录、音频、系统网络配置或固件。不同硬件不可互相恢复。'),backup,backupFile,restore]),
   ]),E('div',{'class':'as-management-stack'},[device.card('通话事件通知',[notifyHint,appearance.switchField('启用通知',enabled),device.field('渠道',provider),device.field('通知地址',endpoint),device.field('PushPlus token',secret),device.field('Telegram chat_id',chat),appearance.switchField('发送电话号码',include),E('p',{},'地址请填写服务提供的完整接收端点。仅支持公网 HTTPS；不发送测试消息，不自动重试。钉钉等开启签名校验的机器人需使用其兼容的转发端点。'),saveNotify]),
   device.card('安装自检',[checks,action('刷新自检',selfcheck),action('修复私有配置权限',function(){return submit('selfcheck_fix',{},'只修复本项目已有配置权限，不伪造模块元数据，不改系统程序。',selfcheck);})]),
   device.card('音频组件',[catalog]),appearance.controls()])])
  ]);return appearance.apply(root);
 },handleSaveApply:null,handleSave:null,handleReset:null
});
