'use strict';
'require baseclass';
'require view.airsim.appearance as appearance';
'require rpc';

// Workflow reference: shangjie1998/airsim-f50-openwrt @ 4df1116.
// This page only reads inventory and composes a command for manual review.
var kernelStatus = rpc.declare({object:'airsim', method:'kernel_status', expect:{}, reject:true});
var profileLabels = {
 profile_match: '内核及配置匹配已验证的 U30 Air 音频方案；仍需确认安装与实际声音。',
 unsupported_kernel: '当前内核没有本项目已验证的音频包。',
 config_unknown: '无法读取内核配置，不能确认音频包兼容性。',
 config_mismatch: '内核版本相同，但配置不匹配，不能使用随包音频扩展。'
};
var audioLabels = {
 metadata_unknown:'元数据缺失或无效', other_kernel:'属于其他内核', config_unknown:'当前配置无法确认',
 config_mismatch:'配置不匹配', modules_invalid:'模块缺失或异常', vermagic_mismatch:'模块版本或格式不匹配',
 metadata_match_unverified:'配置及模块版本资料匹配；完整性、符号 ABI 与通话仍待验证'
};
function shellQuote(value) { return "'" + value.replace(/'/g, "'\\''") + "'"; }
function makeKernelCommand(state, series, tag, bundle, sha, mode) {
 if (!state || !state.ok || !Array.isArray(state.choices) || !Array.isArray(state.bundles)) return '';
 if (['5.4','6.18','7.3'].indexOf(series) < 0 || !state.choices.some(function(c){return c.series === series;})) return '';
 if (!/^v[0-9][A-Za-z0-9._-]{0,63}$/.test(tag)) return '';
 if (!state.bundles.some(function(b){return b.path === bundle;}) || /[\r\n\x00-\x1f\x7f]/.test(bundle)) return '';
 if (!/^\/(mnt\/mu300-disk\/pkg|tmp)\/[^/]*kernel[^/]*\.tar\.gz$/.test(bundle)) return '';
 if (series === '7.3') {
  if (!state.updater_73 || ['native','patched','patch_available'].indexOf(state.updater_73.status) < 0 || !/^[a-f0-9]{64}$/.test(sha || '')) return '';
  var request = {release:tag,bundle:bundle,sha256:sha,confirm:mode === 'apply'};
  return "printf '%s\\n' " + shellQuote(JSON.stringify(request)) + ' | /usr/lib/airsim/airsim-mu300d --kernel-7-3 ' + (mode === 'apply' ? 'apply' : 'check');
 }
 return 'MU300_RELEASE=' + shellQuote(tag) + ' MU300_KERNEL_BUNDLE=' + shellQuote(bundle) + ' mu300-update kernel ' + shellQuote(series);
}
function kernelSeries(kernel) { var m = /^(\d+\.\d+)(?:\.|$)/.exec(kernel || ''); return m ? m[1] : ''; }

return baseclass.extend({
 load:function(){ return kernelStatus().catch(function(){return {ok:false,error:'无法读取内核信息，请检查插件版本与管理服务。'};}); },
 render:function(initial) {
  var state = initial || {}, pending = false;
  var current = E('dd'), boot = E('dd'), installed = E('dd'), digest = E('dd'), bundled = E('dd'), reservation = E('dd'), updater = E('dd');
  var profile = E('p', {'class':'as-copy','role':'status'}), inventory = E('ul', {'class':'as-copy'});
  var selectedHint = E('p', {'class':'as-copy','role':'status'}), feedback = E('p', {'class':'as-caption','role':'status','aria-live':'polite'});
  var target = E('select', {id:'as-kernel-target','class':'as-call-select',change:paint});
  target.appendChild(E('option',{value:''},'请选择目标内核'));
  (state.choices || []).forEach(function(c){target.appendChild(E('option',{value:c.series},c.series));});
  var runningSeries = kernelSeries(state.kernel);
  target.value = (state.choices || []).some(function(c){return c.series === runningSeries;}) ? runningSeries : '';
  var release = E('input', {id:'as-kernel-release','class':'as-dial-input',type:'text',maxlength:65,placeholder:'对应内核包的上游 Release 标签',input:paint});
  var sha = E('input', {id:'as-kernel-sha','class':'as-dial-input',type:'text',maxlength:64,placeholder:'从可信发布清单取得的 SHA-256',input:paint});
  var mode = E('select',{id:'as-kernel-action','class':'as-call-select',change:paint},[
   E('option',{value:'check'},'只检查（默认）'),E('option',{value:'apply'},'写入 7.3 内核（不重启）')
  ]);
  mode.value = 'check';
  // Do not infer a target release from the installed system's tag.
  var bundle = E('select',{id:'as-kernel-bundle','class':'as-call-select',change:paint});
  var command = E('pre', {id:'as-kernel-command',style:'white-space:pre-wrap;overflow-wrap:anywhere;user-select:text;'});
  var copy = E('button', {type:'button','class':'as-button as-secondary',click:async function(){
   if (copy.disabled) return;
   var value = makeKernelCommand(state,target.value,release.value.trim(),bundle.value,sha.value.trim(),mode.value);
   if (!value) return;
   try {
    if (typeof navigator === 'undefined' || !navigator.clipboard) throw new Error('clipboard');
    await navigator.clipboard.writeText(value); feedback.textContent = '已复制。请先核对目标设备和内核包，再到 SSH 执行；重启需另外安排。';
   } catch (_) { feedback.textContent = '浏览器不允许复制，请手动选中上面的命令复制。'; }
  }}, '复制命令');
  var refresh = E('button', {type:'button','class':'as-button as-secondary',click:async function(){
   if (pending) return;
   pending = true; refresh.disabled = true; paint();
   try { state = await kernelStatus(); }
   catch (_) { state = {ok:false,error:'读取失败，不能确认当前内核与音频状态。'}; }
   finally { pending = false; refresh.disabled = false; update(); }
  }}, '刷新状态');
  function row(label, node) { return E('div',{'class':'as-row'},[E('dt',{},label),node]); }
  function field(label, id, node) { return E('div',{'class':'as-setting'},[E('label',{'for':id,'class':'as-field-label'},label),node]); }
  function paint() {
   var choice = (state.choices || []).find(function(c){return c.series === target.value;});
   selectedHint.textContent = choice ? choice.audio : '请选择目标版本；未知内核不会自动替你选择 5.4。';
   var value = pending ? '' : makeKernelCommand(state,target.value,release.value.trim(),bundle.value,sha.value.trim(),mode.value);
   sha.disabled = mode.disabled = target.value !== '7.3';
   command.textContent = value || '请选择目标内核、本机内核包及 Release 标签；7.3 还需已审查的更新器和可信 SHA-256。';
   copy.disabled = !value;
  }
  function update() {
   current.textContent = state.ok ? state.kernel : '未知';
   updater.textContent = ({native:'已审查：原生支持 7.3',patched:'已应用：7.3 补丁与原始备份',patch_available:'已审查：手动写入时备份并补上 7.3',missing:'原生更新器不可读取',unknown:'更新器版本未审查，7.3 命令已禁用'})[(state.updater_73 || {}).status] || '更新器状态未知，7.3 命令已禁用';
   boot.textContent = state.ok ? state.boot_selection || '未提供' : '未知';
   installed.textContent = state.ok ? [state.installed_kernel,state.installed_tag].filter(Boolean).join(' · ') || '未提供' : '未知';
   digest.textContent = state.ok ? state.config_sha256 || '无法读取' : '未知';
   digest.style = 'overflow-wrap:anywhere;';
   profile.textContent = state.ok ? profileLabels[state.profile_status] || '兼容性未知' : state.error || '读取失败';
   bundled.textContent = state.ok ? state.bundled_audio ? '检测到 R16 音频归档；安装时仍需校验固定哈希。' : '未检测到随包音频归档' : '未知';
   reservation.textContent = state.ok && state.reservation_missing !== '' && state.reservation_missing != null ? 'missing=' + state.reservation_missing + '（仅内存预留状态）' : '未读取到内存预留状态';
   inventory.replaceChildren();
   (state.audio || []).forEach(function(a){inventory.appendChild(E('li',{},a.directory + ' · ' + a.modules + ' 个模块 · ' + (audioLabels[a.status] || '未知状态')));});
   if (!(state.audio || []).length) inventory.appendChild(E('li',{},state.ok ? '未发现 AirSIM 扩展目录；不能据此判断系统自带音频是否可用。' : '音频目录状态未知'));
   // Preserve drafts across refresh, including recovery after a failed first read.
   var picked = target.value;
   if (state.ok) {
    target.replaceChildren(E('option',{value:''},'请选择目标内核'));
    (state.choices || []).forEach(function(c){target.appendChild(E('option',{value:c.series},c.series));});
    target.value = (state.choices || []).some(function(c){return c.series === picked;}) ? picked : '';
   }
   var selected = bundle.value;
   bundle.replaceChildren(E('option',{value:''},'请选择已下载并核验的内核包'));
   (state.bundles || []).forEach(function(b){bundle.appendChild(E('option',{value:b.path},b.path + ' · ' + (b.size/1048576).toFixed(1) + ' MB'));});
   bundle.value = (state.bundles || []).some(function(b){return b.path === selected;}) ? selected : '';
   if (!state.ok) feedback.textContent = '状态不可用，命令生成已暂停。';
   else feedback.textContent = '刷新只读取状态，保留你的版本与标签选择。';
   paint();
  }
  update();
  return E('div',{'class':'as-root as-kernel'},[
   E('link',{rel:'stylesheet',href:L.resource('view/airsim/overview.css')}),
   E('header',{'class':'as-header'},[E('div',{},[E('h2',{},'内核与音频'),E('p',{'class':'as-copy'},'查看当前内核、匹配音频方案，准备内核切换命令。')]),refresh]),
   E('section',{'class':'as-card'},[
    E('h3',{},'当前设备'),E('dl',{},[row('运行内核',current),row('启动选择',boot),row('已安装记录',installed),row('7.3 更新器',updater),row('配置 SHA-256',digest),row('随包音频',bundled),row('音频内存',reservation)]),profile,
    E('p',{'class':'as-caption'},'实际运行版本以运行内核为准；启动记录可能尚未生效。'),
    E('h3',{},'本机音频扩展'),inventory,
    E('p',{'class':'as-caption'},'只读检查配置及模块版本资料，不加载驱动、不启动 DSP，也不能替代双向通话验收。')
   ]),
   E('section',{'class':'as-card'},[
    E('h3',{},'选择内核版本'),field('目标内核','as-kernel-target',target),selectedHint,
    field('上游 Release 标签','as-kernel-release',release),field('本机内核包','as-kernel-bundle',bundle),
    field('7.3 内核包 SHA-256','as-kernel-sha',sha),field('7.3 操作','as-kernel-action',mode),
    E('p',{'class':'as-caption'},'7.3 默认生成只检查命令。写入前请先关闭 AirSIM 服务；机型、包校验、通话空闲和原生回退保护仍会检查。7.3 音频尚未在 U30 验收。'),
    E('p',{'class':'as-caption'},'从 /mnt/mu300-disk/pkg/ 和 /tmp/ 列出内核归档。文件名和大小不代表内容匹配；请自行核对来源、校验值及目标硬件。'),
    command,copy,feedback,
    E('p',{'class':'as-copy'},'沿用参考项目的操作方式：页面只生成命令，切换需在 SSH 手动执行并安排重启。切换会修改系统内核，请先结束通话并备好回退包。'),
    E('p',{'class':'as-caption'},'本页未引入其他内核的音频包；现有签名安装与内核配置校验继续生效。')
   ])
  ]);
 }, handleSaveApply:null, handleSave:null, handleReset:null
});
