import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequestGuard } from '../src/requestGuard.ts';

test('an older directory response cannot replace the latest response', () => {
  const guard = createRequestGuard();
  const old = guard.begin();
  const latest = guard.begin();
  assert.equal(guard.current(old), false);
  assert.equal(guard.current(latest), true);
});

test('unmount and device change invalidate requests and delayed dialogs', () => {
  const deviceA = createRequestGuard();
  const old = deviceA.begin();
  deviceA.dispose();
  const deviceB = createRequestGuard();
  assert.equal(deviceA.current(old), false);
  assert.equal(deviceA.active(), false);
  assert.equal(deviceB.current(deviceB.begin()), true);
});

test('StrictMode remount does not revive an old request', () => {
  const guard = createRequestGuard();
  const old = guard.begin();
  guard.dispose();
  guard.activate();
  assert.equal(guard.current(old), false);
  assert.equal(guard.current(guard.begin()), true);
});
