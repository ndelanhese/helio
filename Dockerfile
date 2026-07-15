# syntax=docker/dockerfile:1.10

FROM node:24.17.0-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.5-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webui/dist/ /src/web-dist/
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -R /src/web-dist/. internal/webui/dist/
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/helio ./cmd/helio
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/helio-healthcheck

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/helio /usr/local/bin/helio
COPY --from=build /out/healthcheck /usr/local/bin/helio-healthcheck
USER 65532:65532
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 CMD ["helio-healthcheck"]
ENTRYPOINT ["helio"]
