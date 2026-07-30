import { Buffer } from 'node:buffer';
import { AlexaDataStoreClient } from './alexa-client.js';
import { createApp } from './app.js';
import { StateStore } from './state.js';

const port = Number(process.env.PORT ?? 3000);
const sharedSecret = decodeSecret(process.env.HELIO_SHARED_SECRET ?? '');
const skillId = required('ALEXA_SKILL_ID');
const state = await new StateStore(process.env.STATE_FILE ?? '/data/state.json').load();
const dataStore = new AlexaDataStoreClient({
  clientId: required('ALEXA_CLIENT_ID'),
  clientSecret: required('ALEXA_CLIENT_SECRET'),
  endpoint: process.env.ALEXA_DATASTORE_ENDPOINT,
  timezone: process.env.HELIO_TIMEZONE,
});
const app = createApp({ state, sharedSecret, skillId, dataStore });
const server = app.listen(port, '0.0.0.0', () => {
  console.log(`helio-alexa relay listening on :${port}`);
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)));
}

function decodeSecret(value) {
  const decoded = Buffer.from(value, 'base64');
  if (decoded.length < 32 || decoded.toString('base64').replace(/=+$/, '') !== value.replace(/=+$/, '')) {
    throw new Error('HELIO_SHARED_SECRET must be valid base64 containing at least 32 bytes');
  }
  return decoded;
}

function required(name) {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}
