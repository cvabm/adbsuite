import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequestGuard } from '../src/requestGuard.ts';
import { loadPackageDetails, mergePackageMetadata } from '../src/packageLoading.ts';

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const rows = (count) => Array.from({ length: count }, (_, i) => ({
  name: `pkg.${i}`, path: `/apps/${i}.apk`, labelPending: true,
}));

test('versions publish before slow labels, names publish in small batches', async () => {
  const nameResult = deferred();
  const versionResult = deferred();
  const batches = [];
  const published = [];
  const loading = loadPackageDetails(rows(50), () => true, async (batch) => {
    batches.push(batch.length);
    if (batches.length === 1) await nameResult.promise;
    return batch;
  }, () => versionResult.promise, (kind) => published.push(kind));
  versionResult.resolve(rows(50));
  await new Promise(setImmediate);
  assert.deepEqual(published, ['versions']);
  nameResult.resolve();
  assert.deepEqual(await loading, []);
  assert.deepEqual(batches, [24, 24, 2]);
  assert.deepEqual(published, ['versions', 'labels', 'labels', 'labels']);
});

test('refresh or device change drops stale results and cancels remaining batches', async () => {
  for (const invalidate of ['refresh', 'device', 'unmount']) {
    const guard = createRequestGuard();
    const ticket = guard.begin();
    const labels = deferred();
    const versions = deferred();
    let batches = 0, publishes = 0;
    const loading = loadPackageDetails(rows(50), () => guard.current(ticket), () => {
      batches++;
      return labels.promise;
    }, () => versions.promise, () => publishes++);
    if (invalidate === 'refresh') guard.begin();
    else guard.dispose();
    labels.resolve(rows(24));
    versions.reject(new Error('old device disconnected'));
    assert.deepEqual(await loading, []);
    assert.equal(batches, 1);
    assert.equal(publishes, 0);
  }
});

test('cached labels need no APK calls; a version failure keeps the displayed list', async () => {
  let labelCalls = 0;
  const list = [{ name: 'pkg', label: 'cached', path: '/base.apk' }];
  const errors = await loadPackageDetails(list, () => true, async () => {
    labelCalls++;
    return [];
  }, async () => { throw new Error('offline'); }, () => assert.fail('unexpected publication'));
  assert.equal(labelCalls, 0);
  assert.match(errors[0], /offline/);
  assert.equal(list[0].label, 'cached');
});

test('metadata cannot undo mutations, resurrect removed apps or overwrite another field', () => {
  const current = [{ name: 'pkg', path: '/base.apk', disabled: true, uninstalled: true,
    label: 'old', versionName: '2.0', versionCode: 2 }];
  const labels = [{ name: 'pkg', path: '/base.apk', disabled: false, uninstalled: false,
    label: 'new' }, { name: 'removed', path: '/removed.apk', label: 'removed' }];
  const merged = mergePackageMetadata(current, labels, 'labels');
  assert.equal(merged.length, 1);
  assert.equal(merged[0].disabled, true);
  assert.equal(merged[0].uninstalled, true);
  assert.equal(merged[0].label, 'new');
  assert.equal(merged[0].versionName, '2.0');
  const versioned = mergePackageMetadata(merged, [{ name: 'pkg', path: '/base.apk',
    label: 'stale', versionName: '3.0', versionCode: 3 }], 'versions');
  assert.equal(versioned[0].label, 'new');
  assert.equal(versioned[0].versionName, '3.0');
  assert.deepEqual(mergePackageMetadata(current, [{ name: 'pkg', path: '/new.apk',
    label: 'wrong installation' }], 'labels'), current);
});
