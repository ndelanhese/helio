const tokenURL = 'https://api.amazon.com/auth/o2/token';
const namespace = 'helio';
const key = 'snapshot';

export class AlexaDataStoreClient {
  #token = null;

  constructor({
    clientId,
    clientSecret,
    endpoint = 'https://api.amazonalexa.com',
    timezone = 'America/Sao_Paulo',
    fetcher = fetch,
  }) {
    this.clientId = clientId;
    this.clientSecret = clientSecret;
    this.endpoint = endpoint.replace(/\/+$/, '');
    this.timezone = timezone;
    this.fetcher = fetcher;
  }

  configured() {
    return Boolean(this.clientId && this.clientSecret);
  }

  async push(snapshot, devices) {
    if (!snapshot || devices.length === 0 || !this.configured()) {
      return { attempted: 0, delivered: 0 };
    }
    let attempted = 0;
    let delivered = 0;
    for (let offset = 0; offset < devices.length; offset += 20) {
      const batch = devices.slice(offset, offset + 20);
      const results = await this.#send(snapshot, batch);
      attempted += batch.length;
      delivered += results.filter((result) => result.type === 'SUCCESS').length;
    }
    return { attempted, delivered };
  }

  async #send(snapshot, devices, retry = true) {
    const token = await this.#accessToken();
    const response = await this.fetcher(`${this.endpoint}/v1/datastore/commands`, {
      method: 'POST',
      headers: {
        authorization: `Bearer ${token}`,
        'content-type': 'application/json',
      },
      body: JSON.stringify({
        commands: [{
          type: 'PUT_OBJECT',
          namespace,
          key,
          content: displaySnapshot(snapshot, this.timezone),
        }],
        target: { type: 'DEVICES', items: devices },
      }),
      signal: AbortSignal.timeout(10_000),
    });
    if (response.status === 401 && retry) {
      this.#token = null;
      return this.#send(snapshot, devices, false);
    }
    if (!response.ok) {
      throw new Error(`Alexa Data Store returned HTTP ${response.status}`);
    }
    const body = await response.json();
    return Array.isArray(body.results) ? body.results : [];
  }

  async #accessToken() {
    if (this.#token && this.#token.expiresAt > Date.now()) {
      return this.#token.value;
    }
    const response = await this.fetcher(tokenURL, {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded;charset=UTF-8' },
      body: new URLSearchParams({
        grant_type: 'client_credentials',
        client_id: this.clientId,
        client_secret: this.clientSecret,
        scope: 'alexa::datastore',
      }),
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) {
      throw new Error(`Login with Amazon returned HTTP ${response.status}`);
    }
    const body = await response.json();
    if (typeof body.access_token !== 'string' || !Number.isFinite(body.expires_in)) {
      throw new Error('Login with Amazon returned an invalid token');
    }
    this.#token = {
      value: body.access_token,
      expiresAt: Date.now() + Math.max(60, body.expires_in - 60) * 1000,
    };
    return this.#token.value;
  }
}

export function displaySnapshot(snapshot, timezone) {
  const stale = snapshot.stale === true;
  const normal = snapshot.status === 'normal';
  return {
    power: formatEnergy(snapshot.acPowerW, 'W', 'kW'),
    energyToday: formatEnergy(snapshot.energyTodayWh, 'Wh', 'kWh'),
    status: stale ? 'Dados desatualizados' : normal ? 'Sistema normal' : 'Verificar sistema',
    statusColor: stale ? '#D6A84B' : normal ? '#56C596' : '#F07A6A',
    updated: `Atualizado ${new Intl.DateTimeFormat('pt-BR', {
      timeZone: timezone,
      hour: '2-digit',
      minute: '2-digit',
    }).format(new Date(snapshot.observedAt))}`,
    observedAt: snapshot.observedAt,
    stale,
  };
}

function formatEnergy(value, baseUnit, kiloUnit) {
  if (value < 1000) {
    return `${new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 0 }).format(value)} ${baseUnit}`;
  }
  return `${new Intl.NumberFormat('pt-BR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value / 1000)} ${kiloUnit}`;
}
