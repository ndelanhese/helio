# syntax=docker/dockerfile:1.10

FROM node:24.17.0-bookworm-slim@sha256:862263c612aa437e3037674b85419622a9d93bff80aa1eee5398dfe686375532 AS dependencies
WORKDIR /app
COPY integrations/alexa-relay/package.json integrations/alexa-relay/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --omit=dev

FROM node:24.17.0-bookworm-slim@sha256:862263c612aa437e3037674b85419622a9d93bff80aa1eee5398dfe686375532
ENV NODE_ENV=production PORT=3000 STATE_FILE=/data/state.json
WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY integrations/alexa-relay/package.json ./
COPY integrations/alexa-relay/src ./src
COPY integrations/alexa-relay/public ./public
RUN mkdir /data && chown node:node /data
USER node
EXPOSE 3000
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:3000/healthz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"]
ENTRYPOINT ["node", "src/server.js"]
