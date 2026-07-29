import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const emptyState = () => ({ version: 1, snapshot: null, devices: [] });

export class StateStore {
  #data = emptyState();
  #writeQueue = Promise.resolve();

  constructor(file) {
    this.file = file;
  }

  async load() {
    await mkdir(dirname(this.file), { recursive: true });
    try {
      const parsed = JSON.parse(await readFile(this.file, 'utf8'));
      if (parsed.version !== 1 || !Array.isArray(parsed.devices)) {
        throw new Error('unsupported state format');
      }
      this.#data = {
        version: 1,
        snapshot: parsed.snapshot ?? null,
        devices: [...new Set(parsed.devices.filter(validDeviceId))].slice(0, 20),
      };
    } catch (error) {
      if (error.code !== 'ENOENT') {
        throw new Error(`load state: ${error.message}`);
      }
    }
    return this;
  }

  snapshot() {
    return this.#data.snapshot ? structuredClone(this.#data.snapshot) : null;
  }

  devices() {
    return [...this.#data.devices];
  }

  async updateSnapshot(snapshot) {
    const current = this.#data.snapshot;
    if (current && Date.parse(current.observedAt) > Date.parse(snapshot.observedAt)) {
      return false;
    }
    if (current && JSON.stringify(current) === JSON.stringify(snapshot)) {
      return false;
    }
    this.#data.snapshot = structuredClone(snapshot);
    await this.#save();
    return true;
  }

  async addDevice(deviceId) {
    if (!validDeviceId(deviceId) || this.#data.devices.includes(deviceId)) {
      return false;
    }
    if (this.#data.devices.length >= 20) {
      throw new Error('device limit reached');
    }
    this.#data.devices.push(deviceId);
    await this.#save();
    return true;
  }

  async removeDevice(deviceId) {
    const devices = this.#data.devices.filter((item) => item !== deviceId);
    if (devices.length === this.#data.devices.length) {
      return false;
    }
    this.#data.devices = devices;
    await this.#save();
    return true;
  }

  async #save() {
    const content = `${JSON.stringify(this.#data)}\n`;
    const temporary = `${this.file}.tmp`;
    this.#writeQueue = this.#writeQueue.then(async () => {
      await writeFile(temporary, content, { mode: 0o600 });
      await rename(temporary, this.file);
    });
    await this.#writeQueue;
  }
}

function validDeviceId(value) {
  return typeof value === 'string'
    && value.startsWith('amzn1.ask.device.')
    && value.length <= 512;
}
