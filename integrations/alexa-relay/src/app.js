import { createHmac, timingSafeEqual } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import express from 'express';
import { rateLimit } from 'express-rate-limit';
import Alexa from 'ask-sdk-core';
import { ExpressAdapter } from 'ask-sdk-express-adapter';

const packageId = 'HelioWidget';
const sourceDirectory = dirname(fileURLToPath(import.meta.url));

export function createApp({ state, sharedSecret, skillId, dataStore, logger = console, now = Date.now }) {
  if (!Buffer.isBuffer(sharedSecret) || sharedSecret.length < 32) {
    throw new Error('shared secret must contain at least 32 bytes');
  }
  if (typeof skillId !== 'string' || !skillId.startsWith('amzn1.ask.skill.')) {
    throw new Error('skill ID is required');
  }
  const app = express();
  const schedulePush = pushScheduler(state, dataStore, logger);
  const ingestAttemptLimiter = rateLimit({
    windowMs: 60_000,
    limit: 120,
    standardHeaders: true,
    legacyHeaders: false,
    keyGenerator: () => 'helio-ingest',
    message: { error: 'rate_limited' },
  });
  const authenticatedIngestLimiter = rateLimit({
    windowMs: 60_000,
    limit: 10,
    standardHeaders: true,
    legacyHeaders: false,
    keyGenerator: () => 'helio-publisher',
    message: { error: 'rate_limited' },
  });

  app.disable('x-powered-by');
  app.use((_, response, next) => {
    response.set({
      'cache-control': 'no-store',
      'content-security-policy': "default-src 'none'",
      'referrer-policy': 'no-referrer',
      'x-content-type-options': 'nosniff',
    });
    next();
  });
  app.get('/healthz', (_, response) => {
    response.json({ status: 'ok' });
  });
  app.get('/privacy', (_, response) => {
    response.type('html').send(`<!doctype html>
<html lang="pt-BR"><meta charset="utf-8"><title>Privacidade — Helio Alexa</title>
<main><h1>Privacidade — Helio Alexa</h1>
<p>Esta integração pessoal recebe somente potência atual, energia gerada no dia, estado e horário da leitura.</p>
<p>Não recebe endereço, localização, serial do inversor, credenciais ou histórico completo do Helio.</p>
<p>Dados ficam no servidor privado do proprietário e no Data Store da skill instalado no dispositivo Alexa.</p></main></html>`);
  });
  app.use('/assets', express.static(join(sourceDirectory, '..', 'public'), {
    fallthrough: false,
    immutable: true,
    maxAge: '1d',
  }));
  app.post(
    '/ingest',
    express.raw({ type: 'application/json', limit: '8kb' }),
    ingestAttemptLimiter,
    authenticateIngest(sharedSecret, now),
    authenticatedIngestLimiter,
    async (request, response) => {
      try {
        const snapshot = parseSnapshot(request.body, now());
        const updated = await state.updateSnapshot(snapshot);
        if (updated) {
          schedulePush();
        }
        response.status(202).json({ accepted: true, updated });
      } catch (error) {
        sendRequestError(response, error);
      }
    },
  );

  const skill = Alexa.SkillBuilders.custom()
    .withSkillId(skillId)
    .addRequestHandlers(
      packageInstalledHandler(state, schedulePush),
      packageRemovedHandler(state),
      dataStoreErrorHandler(logger),
      launchHandler(state),
      helpHandler,
      stopHandler,
      sessionEndedHandler,
    )
    .addErrorHandlers(errorHandler(logger))
    .create();
  const adapter = new ExpressAdapter(skill, true, true);
  app.post('/alexa', adapter.getRequestHandlers());

  app.use((_, response) => response.status(404).json({ error: 'not_found' }));
  app.use((error, _, response, __) => {
    logger.error(`relay request failed: ${error.message}`);
    response.status(500).json({ error: 'internal_error' });
  });
  return app;
}

function authenticateIngest(secret, now) {
  return (request, response, next) => {
    try {
      verifySignature(request, secret, now());
      next();
    } catch (error) {
      sendRequestError(response, error);
    }
  };
}

function sendRequestError(response, error) {
  response.status(error.status ?? 400).json({ error: error.code ?? 'invalid_request' });
}

function verifySignature(request, secret, currentTime) {
  if (!Buffer.isBuffer(request.body)) {
    throw requestError(415, 'invalid_content_type');
  }
  const timestamp = request.get('x-helio-timestamp') ?? '';
  const seconds = Number(timestamp);
  if (!/^\d{10}$/.test(timestamp) || !Number.isSafeInteger(seconds)
    || Math.abs(currentTime - seconds * 1000) > 5 * 60 * 1000) {
    throw requestError(401, 'expired_request');
  }
  const provided = request.get('x-helio-signature') ?? '';
  if (!/^sha256=[a-f0-9]{64}$/.test(provided)) {
    throw requestError(401, 'invalid_signature');
  }
  const expected = `sha256=${createHmac('sha256', secret)
    .update(timestamp)
    .update('.')
    .update(request.body)
    .digest('hex')}`;
  if (!timingSafeEqual(Buffer.from(provided), Buffer.from(expected))) {
    throw requestError(401, 'invalid_signature');
  }
}

function parseSnapshot(raw, currentTime) {
  let value;
  try {
    value = JSON.parse(raw.toString('utf8'));
  } catch {
    throw requestError(400, 'invalid_json');
  }
  const allowed = ['acPowerW', 'energyTodayWh', 'observedAt', 'stale', 'status', 'version'];
  if (!value || typeof value !== 'object' || Array.isArray(value)
    || Object.keys(value).some((key) => !allowed.includes(key))
    || value.version !== 1
    || !finiteBetween(value.acPowerW, 0, 1_000_000)
    || !finiteBetween(value.energyTodayWh, 0, 100_000_000)
    || typeof value.stale !== 'boolean'
    || typeof value.status !== 'string'
    || !/^[a-zA-Z0-9 _-]{1,64}$/.test(value.status)
    || !validDate(value.observedAt)
    || Date.parse(value.observedAt) > currentTime + 5 * 60 * 1000) {
    throw requestError(422, 'invalid_snapshot');
  }
  return {
    version: 1,
    observedAt: new Date(value.observedAt).toISOString(),
    acPowerW: value.acPowerW,
    energyTodayWh: value.energyTodayWh,
    status: value.status,
    stale: value.stale,
  };
}

function packageInstalledHandler(state, schedulePush) {
  return {
    canHandle: (input) => Alexa.getRequestType(input.requestEnvelope)
      === 'Alexa.DataStore.PackageManager.UsagesInstalled',
    handle: async (input) => {
      await state.addDevice(deviceId(input));
      schedulePush();
      return input.responseBuilder.getResponse();
    },
  };
}

function packageRemovedHandler(state) {
  return {
    canHandle: (input) => Alexa.getRequestType(input.requestEnvelope)
      === 'Alexa.DataStore.PackageManager.UsagesRemoved',
    handle: async (input) => {
      await state.removeDevice(deviceId(input));
      return input.responseBuilder.getResponse();
    },
  };
}

function dataStoreErrorHandler(logger) {
  return {
    canHandle: (input) => Alexa.getRequestType(input.requestEnvelope).startsWith('Alexa.DataStore.'),
    handle: (input) => {
      logger.warn(`Alexa Data Store event: ${Alexa.getRequestType(input.requestEnvelope)}`);
      return input.responseBuilder.getResponse();
    },
  };
}

function launchHandler(state) {
  return {
    canHandle: (input) => Alexa.getRequestType(input.requestEnvelope) === 'LaunchRequest'
      || (Alexa.getRequestType(input.requestEnvelope) === 'IntentRequest'
        && Alexa.getIntentName(input.requestEnvelope) === 'GetSolarIntent'),
    handle: (input) => {
      const snapshot = state.snapshot();
      if (!snapshot) {
        return input.responseBuilder
          .speak('Helio ainda não recebeu dados da geração solar.')
          .withShouldEndSession(true)
          .getResponse();
      }
      const power = snapshot.acPowerW < 1000
        ? `${Math.round(snapshot.acPowerW)} watts`
        : `${(snapshot.acPowerW / 1000).toLocaleString('pt-BR', { maximumFractionDigits: 2 })} quilowatts`;
      return input.responseBuilder
        .speak(snapshot.stale
          ? `Dados desatualizados. Última potência registrada: ${power}.`
          : `Geração solar atual: ${power}.`)
        .withShouldEndSession(true)
        .getResponse();
    },
  };
}

const helpHandler = {
  canHandle: (input) => Alexa.getRequestType(input.requestEnvelope) === 'IntentRequest'
    && ['AMAZON.HelpIntent', 'AMAZON.FallbackIntent'].includes(Alexa.getIntentName(input.requestEnvelope)),
  handle: (input) => input.responseBuilder
    .speak('Diga: Alexa, abrir Helio, para ouvir a geração solar atual.')
    .getResponse(),
};

const stopHandler = {
  canHandle: (input) => Alexa.getRequestType(input.requestEnvelope) === 'IntentRequest'
    && ['AMAZON.CancelIntent', 'AMAZON.StopIntent'].includes(Alexa.getIntentName(input.requestEnvelope)),
  handle: (input) => input.responseBuilder.speak('Até logo.').getResponse(),
};

const sessionEndedHandler = {
  canHandle: (input) => Alexa.getRequestType(input.requestEnvelope) === 'SessionEndedRequest',
  handle: (input) => input.responseBuilder.getResponse(),
};

function errorHandler(logger) {
  return {
    canHandle: () => true,
    handle: (input, error) => {
      logger.error(`Alexa request failed: ${error.message}`);
      return input.responseBuilder
        .speak('Não consegui consultar o Helio agora.')
        .withShouldEndSession(true)
        .getResponse();
    },
  };
}

function pushScheduler(state, dataStore, logger) {
  let running = false;
  let pending = false;
  const schedule = () => {
    pending = true;
    if (running) {
      return;
    }
    running = true;
    void (async () => {
      while (pending) {
        pending = false;
        try {
          const result = await dataStore.push(state.snapshot(), state.devices());
          if (result.attempted > 0) {
            logger.info(`Alexa widget update: ${result.delivered}/${result.attempted} delivered`);
          }
        } catch (error) {
          logger.error(`Alexa widget update failed: ${error.message}`);
        }
      }
    })().finally(() => {
      running = false;
      if (pending) {
        setImmediate(schedule);
      }
    });
  };
  return schedule;
}

function deviceId(input) {
  const value = input.requestEnvelope.context?.System?.device?.deviceId;
  if (typeof value !== 'string' || !value.startsWith('amzn1.ask.device.')) {
    throw new Error('Alexa request has no valid device ID');
  }
  return value;
}

function finiteBetween(value, minimum, maximum) {
  return Number.isFinite(value) && value >= minimum && value <= maximum;
}

function validDate(value) {
  return typeof value === 'string'
    && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
    && Number.isFinite(Date.parse(value));
}

function requestError(status, code) {
  return Object.assign(new Error(code), { status, code });
}
