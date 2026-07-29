import { createHmac, randomBytes } from 'node:crypto';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, test } from 'node:test';
import assert from 'node:assert/strict';
import { AlexaDataStoreClient } from '../src/alexa-client.js';
import { createApp } from '../src/app.js';
import { StateStore } from '../src/state.js';

const servers = [];
const skillId = 'amzn1.ask.skill.00000000-0000-0000-0000-000000000000';
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise((resolve) => server.close(resolve)))));

test('ingest accepts signed snapshot and rejects replay timestamp or bad signature', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'helio-alexa-'));
  const state = await new StateStore(join(directory, 'state.json')).load();
  const secret = randomBytes(32);
  const pushes = [];
  const dataStore = {
    configured: () => true,
    push: async (snapshot, devices) => {
      pushes.push({ snapshot, devices });
      return { attempted: 0, delivered: 0 };
    },
  };
  const now = Date.parse('2026-07-29T19:30:00Z');
  const app = createApp({
    state, sharedSecret: secret, skillId, dataStore, now: () => now, logger: quietLogger,
  });
  const server = app.listen(0, '127.0.0.1');
  servers.push(server);
  await new Promise((resolve) => server.once('listening', resolve));
  const endpoint = `http://127.0.0.1:${server.address().port}/ingest`;
  const snapshot = {
    version: 1,
    observedAt: '2026-07-29T19:29:55Z',
    acPowerW: 2840,
    energyTodayWh: 14200,
    status: 'normal',
    stale: false,
  };
  const body = JSON.stringify(snapshot);
  const timestamp = String(now / 1000);

  const accepted = await fetch(endpoint, {
    method: 'POST',
    headers: signedHeaders(secret, timestamp, body),
    body,
  });
  assert.equal(accepted.status, 202);
  assert.deepEqual(state.snapshot(), { ...snapshot, observedAt: '2026-07-29T19:29:55.000Z' });

  const expired = await fetch(endpoint, {
    method: 'POST',
    headers: signedHeaders(secret, String(now / 1000 - 301), body),
    body,
  });
  assert.equal(expired.status, 401);

  const invalid = await fetch(endpoint, {
    method: 'POST',
    headers: { ...signedHeaders(secret, timestamp, body), 'x-helio-signature': `sha256=${'0'.repeat(64)}` },
    body,
  });
  assert.equal(invalid.status, 401);
});

test('relay requires an Alexa skill ID', () => {
  assert.throws(() => createApp({
    state: {}, sharedSecret: randomBytes(32), dataStore: {}, skillId: '',
  }), /skill ID is required/);
});

test('state keeps newest snapshot and installed devices across reload', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'helio-alexa-'));
  const file = join(directory, 'state.json');
  const state = await new StateStore(file).load();
  const newest = snapshotAt('2026-07-29T19:30:00Z');
  assert.equal(await state.updateSnapshot(newest), true);
  assert.equal(await state.updateSnapshot(newest), false);
  assert.equal(await state.updateSnapshot(snapshotAt('2026-07-29T19:29:00Z')), false);
  assert.equal(await state.addDevice('amzn1.ask.device.test'), true);

  const reloaded = await new StateStore(file).load();
  assert.deepEqual(reloaded.snapshot(), newest);
  assert.deepEqual(reloaded.devices(), ['amzn1.ask.device.test']);
});

test('data store client sends formatted snapshot with skill credentials', async () => {
  const calls = [];
  const fetcher = async (url, options) => {
    calls.push({ url, options });
    if (url === 'https://api.amazon.com/auth/o2/token') {
      return Response.json({ access_token: 'token', expires_in: 3600 });
    }
    return Response.json({ results: [{ type: 'SUCCESS' }] });
  };
  const client = new AlexaDataStoreClient({
    clientId: 'client', clientSecret: 'secret', timezone: 'America/Sao_Paulo', fetcher,
  });
  const result = await client.push(snapshotAt('2026-07-29T19:30:00Z'), ['amzn1.ask.device.test']);

  assert.deepEqual(result, { attempted: 1, delivered: 1 });
  assert.equal(calls[0].url, 'https://api.amazon.com/auth/o2/token');
  assert.equal(calls[1].url, 'https://api.amazonalexa.com/v1/datastore/commands');
  const body = JSON.parse(calls[1].options.body);
  assert.deepEqual(body.target, { type: 'DEVICES', items: ['amzn1.ask.device.test'] });
  assert.equal(body.commands[0].namespace, 'helio');
  assert.equal(body.commands[0].key, 'snapshot');
  assert.equal(body.commands[0].content.power, '1 W');
  assert.equal(body.commands[0].content.energyToday, '2 Wh');
});

function signedHeaders(secret, timestamp, body) {
  const signature = createHmac('sha256', secret)
    .update(timestamp)
    .update('.')
    .update(body)
    .digest('hex');
  return {
    'content-type': 'application/json',
    'x-helio-timestamp': timestamp,
    'x-helio-signature': `sha256=${signature}`,
  };
}

function snapshotAt(observedAt) {
  return {
    version: 1,
    observedAt,
    acPowerW: 1,
    energyTodayWh: 2,
    status: 'normal',
    stale: false,
  };
}

const quietLogger = { info() {}, warn() {}, error() {} };
