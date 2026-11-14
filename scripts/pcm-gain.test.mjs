import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';

const source = readFileSync(new URL('../packaging/openwrt/root/etc/init.d/airsim', import.meta.url), 'utf8');

function start(down = '', up = '', saved = false) {
  // All hardware/procd functions are replaced; no service or device is touched.
  const script = source.replace('. /lib/functions.sh', ':')
    .replaceAll('/usr/lib/airsim/airsim-mu300d', 'mock_daemon')
    .replace('[ -f /mnt/mu300-disk/extra/airsim/data/management.json ]', saved ? '[ 1 = 1 ]' : '[ 0 = 1 ]');
  const mocks = `
config_load() { :; }
config_get_bool() { enabled=1; }
config_get() {
  case "$1" in
    interface) interface=br-lan ;;
    pcm_downlink_gain_db) pcm_downlink_gain_db="\${TEST_DOWN:-$4}" ;;
    pcm_uplink_gain_db) pcm_uplink_gain_db="\${TEST_UP:-$4}" ;;
  esac
}
mock_daemon() { echo "DAEMON $*"; }
procd_open_instance() { echo "INSTANCE $*"; }
procd_close_instance() { :; }
procd_set_param() { echo "SET $*"; }
procd_append_param() { echo "APPEND $*"; }
`;
  return spawnSync('/bin/sh', ['-c', mocks + script + '\nstart_service\n'], {
    encoding: 'utf8', env: { PATH: process.env.PATH, TEST_DOWN: down, TEST_UP: up }
  });
}

test('legacy config defaults to unity gain on bridge only', () => {
  const result = start();
  assert.equal(result.status, 0, result.stderr);
  const [agent, bridge] = result.stdout.split('INSTANCE agent')[1].split('INSTANCE bridge');
  assert.ok(!agent.includes('gain-db'));
  assert.match(bridge, /--interface br-lan/);
  assert.match(bridge, /APPEND command --pcm-downlink-gain-db 0 --pcm-uplink-gain-db 0/);
});

test('both legacy and saved-management startup pass independent PCM gains', () => {
  for (const saved of [false, true]) {
    const result = start('6', '3', saved);
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /APPEND command --pcm-downlink-gain-db 6 --pcm-uplink-gain-db 3/);
    assert.equal(result.stdout.includes('--interface br-lan'), !saved);
  }
});

test('invalid UCI gain refuses startup before hardware preparation', () => {
  for (const value of ['-1', '13', '100', '6.5', 'NaN', '1;false', ' 6', '--help']) {
    for (const [down, up] of [[value, '0'], ['0', value]]) {
      const result = start(down, up);
      assert.notEqual(result.status, 0);
      assert.equal(result.stdout, '');
    }
  }
});
