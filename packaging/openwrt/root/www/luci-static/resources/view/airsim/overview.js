'use strict';
'require view';
'require rpc';
'require poll';
'require view.airsim.sms as sms';

var getStatus = rpc.declare({ object: 'airsim', method: 'status', expect: {} });
var callAction = rpc.declare({ object: 'airsim', method: 'call_action', params: ['action', 'number', 'call_id', 'operation_id'], expect: {}, reject: true, nobatch: true });
var actions = {};
['start', 'stop', 'pair_start', 'pair_cancel', 'number_refresh'].forEach(function(name) {
 actions[name] = rpc.declare({ object: 'airsim', method: name, expect: {} });
});

return view.extend({
 load: function() { return getStatus(); },
 render: function(data) {
  var current = data || {}, pending = '', previousVersion = '', callOptions = '', dialAttempt = null;
  var root, nodes = {}, buttons = {};
  function text(key, tag, className, content) {
   nodes[key] = E(tag, { 'class': className || '' }, content || '');
   return nodes[key];
  }
  function set(key, value) { if (nodes[key].textContent !== String(value)) nodes[key].textContent = String(value); }
  function ready(s) { return !!(s.running && s.agent && s.agent.reachable && (!s.audio_setup || s.audio_setup === 'ready')); }
  function callLabel(state) { return ({ incoming: '来电响铃', waiting: '等待接听', dialing: '正在拨号', alerting: '对方振铃', active: '已接通', held: '通话保持' })[state] || '状态待确认'; }
  function callMessage(kind, title, description) {
   nodes.callFeedback.className = 'as-feedback as-feedback-' + kind;
   nodes.callFeedback.hidden = false;
   set('callFeedbackTitle', title); set('callFeedbackDescription', description);
  }
  function syncCalls(s) {
   var state = s.calls || {}, calls = state.calls || [], available = ready(s) && state.available;
   var options = JSON.stringify(calls.map(function(c) { return [c.id, c.number, c.state]; }));
   if (options !== callOptions) {
    var selected = nodes.callSelect.value;
    nodes.callSelect.replaceChildren(E('option', { value: '' }, calls.length ? '选择要操作的通话' : '当前没有通话'));
    calls.forEach(function(c) { nodes.callSelect.appendChild(E('option', { value: c.id }, (c.number || '号码隐藏') + ' · ' + callLabel(c.state))); });
    nodes.callSelect.value = calls.some(function(c) { return c.id === selected; }) ? selected : '';
    callOptions = options;
   }
   var target = calls.find(function(c) { return c.id === nodes.callSelect.value; });
   nodes.callSelect.disabled = !!pending || !available || !calls.length;
   nodes.dialNumber.disabled = !!pending || !available || !!calls.length;
   buttons.dial.disabled = !!pending || !available || !!calls.length || !nodes.dialNumber.value.trim();
   buttons.answer.disabled = !!pending || !available || !target || ['incoming', 'waiting'].indexOf(target.state) < 0;
   buttons.hangup.disabled = !!pending || !available || !target;
   buttons.dial.textContent = pending === 'dial' ? '正在拨号…' : '拨号';
   buttons.answer.textContent = pending === 'answer' ? '正在接听…' : '接听';
   buttons.hangup.textContent = pending === 'hangup' ? '正在挂断…' : '挂断';
   set('callStatus', !available ? state.error || '等待电话服务就绪' : calls.length ? calls.length + ' 路通话 · ' + calls.map(function(c) { return callLabel(c.state); }).join(' / ') : '当前空闲，可以拨号');
   set('call', !available ? '状态待确认' : calls.length ? calls.map(function(c) { return callLabel(c.state); }).join(' / ') : '待机');
  }
  function message(kind, title, description) {
   nodes.feedback.className = 'as-feedback as-feedback-' + kind;
   nodes.feedback.hidden = false;
   set('feedbackTitle', title); set('feedbackDescription', description);
  }
  function row(label, key) { return E('div', { 'class': 'as-row' }, [E('dt', {}, label), text(key, 'dd')]); }
  function button(label, action, className) {
   buttons[action] = E('button', { 'type': 'button', 'class': 'as-button ' + className,
    'click': function() { return ['dial', 'answer', 'hangup'].indexOf(action) >= 0 ? runCall(action) : run(action); } }, label);
   return buttons[action];
  }
  function refresh(s) {
   current = s;
   var online = ready(s), sim = s.sim || {}, audio = s.audio || {}, relay = s.relay || {};
   var setupBlocked = !!s.audio_setup && s.audio_setup !== 'ready';
   var setupCopy = s.audio_setup === 'reboot_required' ? '音频组件已安装，请重启进入 OpenWrt 一次，系统会自动完成准备。' :
    s.audio_setup === 'failed' || s.audio_setup === 'not_prepared' ? '音频安装准备未完成，请检查安装结果；当前还不能通话。' : '音频组件正在准备，请稍候。';
   var paired = s.pairing && s.pairing.expires_at > Date.now() ? s.pairing : null;
   if (s.version) previousVersion = s.version;
   set('version', previousVersion ? 'v' + previousVersion : 'MU300 · OpenWrt');
   set('live', setupBlocked ? s.audio_setup === 'reboot_required' ? '需重启完成安装' : '音频准备未完成' : pending === 'start' ? '正在启动' : pending === 'stop' ? '正在关闭' : online ? '服务运行中' : s.service_alive ? '服务连接中' : '服务已关闭');
   nodes.live.className = 'as-pill ' + (online ? 'as-pill-green' : 'as-pill-neutral');
   set('number', sim.phone_number || (online ? '等待号码' : '—'));
   set('numberHint', !online ? '启动服务后，自动读取 SIM 本机号码。' : sim.phone_number ? 'SIM 自动读取 · ' + (sim.phone_number_updated_at ? new Date(sim.phone_number_updated_at).toLocaleTimeString() + ' 更新' : '实时同步') : sim.phone_number_status === 'not_provided' ? 'SIM 未存储本机号码。可点击右侧重新读取。' : '正在读取，约每 30 秒自动刷新。');
   set('sim', !online ? '等待服务启动' : sim.sim_inserted ? '已就绪' : '未就绪');
   set('network', sim.reg_status_text || '等待检测');
   set('signal', sim.signal_dbm != null ? sim.signal_dbm + ' dBm' : '基带暂未提供');
   set('call', s.call_active ? '通话进行中' : online ? '待机' : '—');
   set('audio', setupBlocked ? s.audio_setup === 'reboot_required' ? '需重启完成准备' : '尚未准备完成' : s.audio_available ? '已识别' : '等待检测');
   set('address', s.lan_ip || '—');
   set('relay', relay.last_error ? '连接暂时异常' : relay.configured ? '已配置' : '尚未配置');
   set('frames', audio.uplink_frames != null ? audio.uplink_frames + ' / ' + audio.downlink_frames : '—');
   set('audioError', audio.last_error === 'EOF' || !audio.last_error ? '无' : audio.last_error);
   set('relayError', relay.last_error || '暂无错误记录');
   set('updated', '状态更新于 ' + new Date().toLocaleTimeString());
   set('serviceCopy', setupBlocked ? setupCopy : online ? '电话网关已就绪，可继续配对客户端。' : '启动电话网关，连接你的 AirSIM 客户端。');
   buttons.start.disabled = !!pending || online || setupBlocked;
   buttons.stop.disabled = !!pending || (!s.service_alive && !s.running) || !!s.call_active;
   buttons.number_refresh.disabled = !!pending || !online;
   buttons.pair_start.disabled = !!pending || !online;
   buttons.pair_cancel.disabled = !!pending || !paired;
   buttons.start.textContent = pending === 'start' ? '正在启动…' : online ? '服务已启动' : '启动服务';
   buttons.stop.textContent = pending === 'stop' ? '正在关闭…' : '关闭服务';
   buttons.pair_start.textContent = pending === 'pair_start' ? '正在获取…' : paired ? '重新获取配对码' : '获取配对码';
   buttons.number_refresh.textContent = pending === 'number_refresh' ? '正在读取…' : '重新读取号码';
   set('stopHint', s.calls && s.calls.available === false ? '通话状态待确认，暂不能关闭服务。' : s.call_active ? '通话结束后才能关闭服务。' : '关闭服务将停止电话网关与 Bonjour 发现。');
   set('discovery', paired ? '配对窗口已开启 · 手动 IP：' + (s.tunnel_ip || s.lan_ip || '未知') + ' · 配对端口：' + s.pairing_port : '配对窗口未开启');
   nodes.discovery.className = 'as-discovery' + (paired ? ' is-active' : '');
   set('code', paired ? paired.code : '— — — — — —');
   set('countdown', paired ? Math.max(0, Math.ceil((paired.expires_at - Date.now()) / 1000)) + ' 秒后失效，可通过局域网自动发现或私网 IP 配对' : online ? '获取配对码时，同步开启 120 秒 Bonjour 发现。' : '请先启动服务，再获取配对码。');
   nodes.code.className = 'as-code' + (paired ? ' is-active' : '');
   syncCalls(s);
   root && root.setAttribute('aria-busy', pending ? 'true' : 'false');
  }
  async function waitFor(predicate, timeout) {
   var deadline = Date.now() + timeout;
   while (Date.now() < deadline) {
    var s = await getStatus(); refresh(s);
    if (predicate(s)) return s;
    await new Promise(function(resolve) { setTimeout(resolve, 1200); });
   }
   throw new Error('等待服务状态确认超时，请检查设备状态后重试。');
  }
  async function run(action) {
   if (pending) return;
   pending = action; refresh(current);
   message('pending', action === 'start' ? '正在启动服务' : action === 'stop' ? '正在关闭服务' : action === 'number_refresh' ? '正在读取 SIM 号码' : '正在更新配对状态', action === 'start' ? '首次启动可能需要约一分钟，请稍候。' : '正在等待设备确认…');
   try {
    var result = await actions[action]();
    if (result.error) throw new Error(result.error);
    if (action === 'start') {
     await waitFor(ready, 90000);
     message('success', '服务启动成功', '下一步：点击下方“获取配对码”，同时开启 Bonjour 发现。');
    } else if (action === 'stop') {
     await waitFor(function(s) { return s.service_alive === false && s.running === false; }, 45000);
     message('success', '服务已成功关闭', '电话网关与 Bonjour 发现已停止。需要连接时，再次启动即可。');
    } else {
     var updated = await getStatus(); refresh(updated);
     if (action === 'pair_start') {
      if (!updated.pairing) throw new Error('未确认配对窗口已开启，请重试。');
      message('success', '配对码已就绪，Bonjour 发现已开启', '在同一局域网的 AirSIM 中添加设备，选择 AirSIM-MU300 并输入配对码。');
     } else if (action === 'pair_cancel') {
      if (updated.pairing) throw new Error('配对窗口仍在运行，请重试。');
      message('success', '配对窗口已关闭', 'Bonjour 配对发现已停止，已有客户端连接不受影响。');
     } else message(result.phone_number ? 'success' : 'info', result.phone_number ? '本机号码已更新' : 'SIM 未提供本机号码', result.phone_number ? '号码已同步到设备状态和经过认证的 AirSIM 网关。' : '该 SIM 未存储本机号码；不会使用来电号码或测试号码代替。');
    }
   } catch (err) {
    message('error', '操作未完成', err.message || '无法连接设备，请稍后重试。');
   } finally { pending = ''; refresh(current); }
  }
  async function runCall(action) {
   if (pending || buttons[action].disabled) return;
   var number = nodes.dialNumber.value.trim().replace(/[ ()-]/g, ''), id = nodes.callSelect.value, operation = '';
   if (action === 'dial') {
    if (!/^\+?[0-9]{3,20}$/.test(number)) { callMessage('error', '号码格式不正确', '请输入 3–20 位数字，可在开头使用 +。'); nodes.dialNumber.focus(); return; }
    if (dialAttempt && dialAttempt.number !== number) { callMessage('error', '上次拨号结果待确认', '请先检查上次通话状态；不会把未确认请求改拨到其他号码。'); return; }
    if (!dialAttempt) {
     // crypto.randomUUID may not be available on OpenWrt's HTTP origin.
     var random = new Uint8Array(16); crypto.getRandomValues(random);
     dialAttempt = { number: number, id: Array.from(random).map(function(n) { return n.toString(16).padStart(2, '0'); }).join('') };
    }
    operation = dialAttempt.id;
   } else if (!id) return;
   pending = action; refresh(current);
   callMessage('pending', action === 'dial' ? '正在发起拨号' : action === 'answer' ? '正在接听所选通话' : '正在挂断所选通话', '正在等待 MU300 确认，请勿重复操作。');
   try {
    var result = await callAction(action, action === 'dial' ? number : '', id, operation);
    if (!result.ok || result.error) throw new Error(result.error || '未收到有效操作回执，请先查看实时状态。');
    if (action === 'dial') dialAttempt = null;
    callMessage('success', action === 'dial' ? '拨号已发起' : action === 'answer' ? '所选通话已接听' : '所选通话已挂断', action === 'dial' ? '请等待对方接听，下方状态会自动更新。浏览器不提供通话音频。' : action === 'answer' ? '请检查 AirSIM 客户端的音频连接；浏览器不传输声音。' : '其他通话不会被自动挂断。');
    try { refresh(await getStatus()); } catch (_) { callMessage('info', '操作已确认，状态刷新暂不可用', '请等待自动刷新，不要重复拨号。'); }
   } catch (err) {
    callMessage('error', '通话操作未完成', (err.message || '网络请求失败') + (action === 'dial' ? ' 若重试相同号码，将复用本次操作编号，不重复发送拨号。' : ''));
   } finally { pending = ''; refresh(current); }
  }
  nodes.feedback = E('div', { 'class': 'as-feedback', 'role': 'status', 'aria-live': 'polite', 'hidden': true }, [
   text('feedbackTitle', 'strong'), text('feedbackDescription', 'p')
  ]);
  nodes.callFeedback = E('div', { 'class': 'as-feedback', 'role': 'status', 'aria-live': 'polite', 'hidden': true }, [text('callFeedbackTitle', 'strong'), text('callFeedbackDescription', 'p')]);
  nodes.dialNumber = E('input', { id: 'as-dial-number', type: 'tel', inputmode: 'tel', autocomplete: 'off', maxlength: 32, placeholder: '输入电话号码', 'class': 'as-dial-input', 'aria-describedby': 'as-dial-help', input: function() { syncCalls(current); }, keydown: function(event) { if (event.key === 'Enter') { event.preventDefault(); runCall('dial'); } } });
  nodes.callSelect = E('select', { id: 'as-call-select', 'class': 'as-call-select', change: function() { syncCalls(current); } }, [E('option', { value: '' }, '当前没有通话')]);
  root = E('div', { 'class': 'as-root' }, [
   E('link', { 'rel': 'stylesheet', 'href': L.resource('view/airsim/overview.css') }),
   E('header', { 'class': 'as-header' }, [E('div', {}, [E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 电话网关'), E('h2', {}, '让连接，简单一点。'), E('p', {}, 'MU300 的运行状态与客户端连接，在这里管理。')]), text('version', 'span', 'as-version')]),
   E('div', { 'class': 'as-layout' }, [
    E('section', { 'class': 'as-card as-status', 'aria-label': '设备状态' }, [
     E('div', { 'class': 'as-card-heading' }, [E('h3', {}, '设备状态'), text('live', 'span', 'as-pill')]),
     E('div', { 'class': 'as-number-block' }, [E('span', { 'class': 'as-label' }, 'SIM 本机号码'), text('number', 'div', 'as-number'), text('numberHint', 'p', 'as-caption')]),
     E('dl', { 'class': 'as-status-list' }, [row('SIM 卡', 'sim'), row('移动网络', 'network'), row('信号强度', 'signal'), row('当前通话', 'call'), row('音频设备', 'audio'), row('设备地址', 'address')]),
     E('details', { 'class': 'as-details' }, [E('summary', {}, '连接与诊断'), E('dl', {}, [row('云端 Relay', 'relay'), row('上行 / 下行帧', 'frames'), row('最近音频错误', 'audioError')]), text('relayError', 'p', 'as-technical')]),
     text('updated', 'p', 'as-updated')
    ]),
    E('div', { 'class': 'as-actions' }, [
     E('section', { 'class': 'as-card', 'aria-label': '服务控制' }, [E('div', { 'class': 'as-step-heading' }, [E('span', { 'class': 'as-step' }, '01'), E('h3', {}, '启动服务')]), text('serviceCopy', 'p', 'as-copy'),
      E('div', { 'class': 'as-buttons' }, [button('启动服务', 'start', 'as-primary'), button('关闭服务', 'stop', 'as-secondary')]),
      text('stopHint', 'p', 'as-caption'), nodes.feedback,
      button('重新读取号码', 'number_refresh', 'as-text-button')
     ]),
     E('section', { 'class': 'as-card as-pair', 'aria-label': '配对客户端' }, [
      E('div', { 'class': 'as-step-heading' }, [E('span', { 'class': 'as-step' }, '02'), E('h3', {}, '配对客户端')]),
      E('p', { 'class': 'as-copy' }, '让 iPhone 与 MU300 处于同一局域网。'),
      E('div', { 'class': 'as-code-area' }, [text('discovery', 'p', 'as-discovery'), text('code', 'div', 'as-code'), text('countdown', 'p', 'as-caption')]),
      E('div', { 'class': 'as-buttons' }, [button('获取配对码', 'pair_start', 'as-primary'), button('关闭配对', 'pair_cancel', 'as-secondary')]),
      E('ol', { 'class': 'as-instructions' }, [E('li', {}, '在 AirSIM 中打开“添加设备”。'), E('li', {}, '选择自动发现的 AirSIM-MU300。'), E('li', {}, '输入上方六位配对码，完成连接。')])
     ])
    ]),
    E('section', { 'class': 'as-card as-dial', 'aria-label': '拨号与通话' }, [
     E('div', { 'class': 'as-step-heading' }, [E('span', { 'class': 'as-step' }, '03'), E('h3', {}, '拨号与通话')]),
     E('p', { 'class': 'as-copy' }, '使用 MU300 的 SIM 卡发起电话。'),
     E('label', { 'for': 'as-dial-number', 'class': 'as-field-label' }, '拨打号码'),
     E('div', { 'class': 'as-dial-entry' }, [nodes.dialNumber, button('拨号', 'dial', 'as-primary')]),
     E('p', { id: 'as-dial-help', 'class': 'as-caption' }, '可能产生运营商通话费用。本页仅控制通话，不提供浏览器音频。'),
     text('callStatus', 'p', 'as-call-status'),
     E('label', { 'for': 'as-call-select', 'class': 'as-field-label' }, '选择通话'), nodes.callSelect,
     E('div', { 'class': 'as-buttons' }, [button('接听', 'answer', 'as-primary'), button('挂断', 'hangup', 'as-danger')]),
     nodes.callFeedback
    ]),
    sms.render()
   ]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for MU300 · 开发预览版')
  ]);
  refresh(current);
  poll.add(function() {
   if (pending) return Promise.resolve();
   return getStatus().then(refresh).catch(function() { current = Object.assign({}, current, { calls: { available: false, error: '状态更新失败，通话操作已暂停' } }); syncCalls(current); message('error', '状态更新失败', '无法连接设备，请检查网络。'); });
  }, 3);
  return root;
 },
 handleSaveApply: null,
 handleSave: null,
 handleReset: null
});
