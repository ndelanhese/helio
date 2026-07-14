# Helio Foundation and Read-Only Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce an executable Helio shell and a fixture-tested, read-only Solarman V5 client that decodes SOFAR 6KTLM-G3 telemetry.

**Architecture:** Go owns runtime and embeds Vite output. Protocol framing and TCP transport remain separate from SOFAR register decoding behind a read-only `RegisterReader`; domain code cannot construct Modbus write requests.

**Tech Stack:** Go 1.26.5, Node.js 24 LTS, React 19.2.7, TypeScript 7.0.2, Vite 8.1.4, TanStack Router 1.170.18, standard-library TCP/HTTP/testing.

## Global Constraints

- Go toolchain: `1.26.5`; module: `github.com/ndelanhese/helio`.
- Node runtime: `24.17.0`; npm lockfile version 3; `npm ci` only in CI/container builds.
- HTTP listens on `:8080` by default and honors `HELIO_HTTP_ADDR`.
- Logger defaults: TCP port `8899`, Modbus slave ID `1`; IP and serial have no source default.
- Logger serial is an unsigned 32-bit integer at protocol boundary and a decimal string in UI/API configuration.
- Every protocol API is read-only. Function code `0x03` is only supported Modbus operation.
- Real-hardware test runs only when `HELIO_HARDWARE_TEST=1` and reads logger settings from environment.
- All user-visible timestamps use configured IANA timezone; stored timestamps use UTC.

---

## File Structure

- `cmd/helio/main.go` — process entrypoint and signal handling.
- `internal/app/app.go` — dependency assembly and HTTP lifecycle.
- `internal/config/config.go` — environment-only process configuration.
- `internal/domain/telemetry.go` — stable typed telemetry shared by collector/API.
- `internal/httpserver/router.go`, `health.go`, `spa.go` — initial HTTP surface and embedded UI fallback.
- `internal/webui/assets.go`, `dist/index.html` — embedded frontend assets and minimal pre-build shell.
- `internal/solarman/frame.go`, `crc.go`, `transport.go`, `client.go` — V5 framing, Modbus CRC, I/O, serialized reads.
- `internal/sofar/registers.go`, `decoder.go` — register blocks and typed SOFAR scaling.
- `internal/solarman/testdata/*.hex` — sanitized deterministic wire fixtures.
- `cmd/helio-hardware-test/main.go` — explicit opt-in read-only hardware probe.
- `web/*` — Vite/React source with generated TanStack route tree.

### Task 1: Executable Go and React Shell

**Files:**
- Create: `go.mod`
- Create: `cmd/helio/main.go`
- Create: `internal/config/config.go`
- Create: `internal/app/app.go`
- Create: `internal/httpserver/router.go`
- Create: `internal/httpserver/health.go`
- Create: `internal/httpserver/spa.go`
- Create: `internal/httpserver/router_test.go`
- Create: `internal/webui/assets.go`
- Create: `internal/webui/dist/index.html`
- Create: `web/package.json`, `web/package-lock.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/index.html`
- Create: `web/src/main.tsx`, `web/src/routes/__root.tsx`, `web/src/routes/index.tsx`, generated `web/src/routeTree.gen.ts`
- Create: `Makefile`

**Interfaces:**
- Consumes: none.
- Produces: `config.Load() config.Config`; `app.New(config.Config) *app.App`; `(*App).Run(context.Context) error`; `httpserver.New(httpserver.Dependencies) http.Handler`.

- [ ] **Step 1: Write failing HTTP contract test**

```go
package httpserver_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ndelanhese/helio/internal/httpserver"
)

func TestHealthAndSPAFallback(t *testing.T) {
    handler := httpserver.New(httpserver.Dependencies{})
    for _, tc := range []struct{ path, contentType string }{
        {"/health/live", "application/json"},
        {"/history", "text/html; charset=utf-8"},
    } {
        req := httptest.NewRequest(http.MethodGet, tc.path, nil)
        rec := httptest.NewRecorder()
        handler.ServeHTTP(rec, req)
        if rec.Code != http.StatusOK { t.Fatalf("%s: got %d", tc.path, rec.Code) }
        if got := rec.Header().Get("Content-Type"); got != tc.contentType { t.Fatalf("%s: %q", tc.path, got) }
    }
}
```

- [ ] **Step 2: Run test and confirm missing package failure**

Run: `go test ./internal/httpserver`

Expected: FAIL containing `no required module provides package` or missing `go.mod`.

- [ ] **Step 3: Create minimal executable shell**

Use module declaration:

```go
module github.com/ndelanhese/helio

go 1.26.5
```

Implement `internal/httpserver/router.go`:

```go
package httpserver

import (
    "encoding/json"
    "io/fs"
    "net/http"
    "path"
    "strings"

    "github.com/ndelanhese/helio/internal/webui"
)

type Dependencies struct{ Ready func() error }

func New(d Dependencies) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { jsonResponse(w, 200, map[string]string{"status":"ok"}) })
    mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
        if d.Ready != nil && d.Ready() != nil { jsonResponse(w, 503, map[string]string{"status":"unavailable"}); return }
        jsonResponse(w, 200, map[string]string{"status":"ready"})
    })
    assets, err := fs.Sub(webui.Assets, "dist")
    if err != nil { panic(err) }
    files := http.FileServer(http.FS(assets))
    mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
        clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
        if clean != "." { if f, err := assets.Open(clean); err == nil { _ = f.Close(); files.ServeHTTP(w, r); return } }
        index, err := fs.ReadFile(assets, "index.html")
        if err != nil { http.Error(w, "UI unavailable", 500); return }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _, _ = w.Write(index)
    })
    return securityHeaders(mux)
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Referrer-Policy", "same-origin")
        w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'")
        next.ServeHTTP(w, r)
    })
}
```

Implement assets and config:

```go
package webui

import "embed"

//go:embed dist/*
var Assets embed.FS
```

```go
package config

import "os"

type Config struct{ HTTPAddr string }

func Load() Config {
    addr := os.Getenv("HELIO_HTTP_ADDR")
    if addr == "" { addr = ":8080" }
    return Config{HTTPAddr: addr}
}
```

Implement app lifecycle and main:

```go
package app

import (
    "context"
    "errors"
    "net/http"
    "time"

    "github.com/ndelanhese/helio/internal/config"
    "github.com/ndelanhese/helio/internal/httpserver"
)

type App struct{ server *http.Server }

func New(cfg config.Config) *App {
    return &App{server:&http.Server{Addr:cfg.HTTPAddr, Handler:httpserver.New(httpserver.Dependencies{}), ReadHeaderTimeout:5*time.Second}}
}

func (a *App) Run(ctx context.Context) error {
    errc := make(chan error, 1)
    go func(){ errc <- a.server.ListenAndServe() }()
    select {
    case err := <-errc:
        if errors.Is(err, http.ErrServerClosed) { return nil }; return err
    case <-ctx.Done():
        shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
        return a.server.Shutdown(shutdown)
    }
}
```

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"

    "github.com/ndelanhese/helio/internal/app"
    "github.com/ndelanhese/helio/internal/config"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    if err := app.New(config.Load()).Run(ctx); err != nil { log.Fatal(err) }
}
```

Create Vite project with exact dependency versions from this plan, file-based TanStack Router, `/api` dev proxy to `http://localhost:8080`, and index route rendering `Helio`. Generate `package-lock.json` using `npm install --package-lock-only` and route tree using `npm run build`.

- [ ] **Step 4: Verify both builds and HTTP contract**

Run: `go test ./... && npm --prefix web ci && npm --prefix web run build && go build ./cmd/helio`

Expected: all Go tests PASS, npm exits 0, `web/dist/index.html` exists, Go build exits 0.

- [ ] **Step 5: Commit shell**

```bash
git add go.mod cmd internal web Makefile
git commit -m "feat: scaffold Helio runtime and React shell"
```

### Task 2: Solarman V5 and Modbus Read Framing

**Files:**
- Create: `internal/solarman/crc.go`, `crc_test.go`
- Create: `internal/solarman/frame.go`, `frame_test.go`
- Create: `internal/solarman/testdata/read_holding_request.hex`
- Create: `internal/solarman/testdata/read_holding_response.hex`

**Interfaces:**
- Consumes: unsigned logger serial and Modbus slave ID from validated configuration.
- Produces: `BuildReadRequest(serial uint32, sequence uint16, slave byte, start, count uint16) ([]byte, error)`; `ParseReadResponse(frame []byte, expectedSerial uint32, expectedSequence uint16) ([]uint16, error)`; `CRC16([]byte) uint16`.

- [ ] **Step 1: Write framing tests from sanitized fixtures**

```go
package solarman

import (
    "encoding/hex"
    "os"
    "strings"
    "testing"
)

func fixture(t *testing.T, name string) []byte {
    t.Helper(); raw, err := os.ReadFile("testdata/"+name); if err != nil { t.Fatal(err) }
    b, err := hex.DecodeString(strings.Join(strings.Fields(string(raw)), "")); if err != nil { t.Fatal(err) }; return b
}

func TestCRC16KnownModbusVector(t *testing.T) {
    b, _ := hex.DecodeString("01030000000a")
    if got := CRC16(b); got != 0xCDC5 { t.Fatalf("got %04x", got) }
}

func TestBuildReadRequest(t *testing.T) {
    got, err := BuildReadRequest(123456789, 7, 1, 0x0000, 10)
    if err != nil { t.Fatal(err) }
    want := fixture(t, "read_holding_request.hex")
    if hex.EncodeToString(got) != hex.EncodeToString(want) { t.Fatalf("got %x want %x", got, want) }
}

func TestParseRejectsWrongSerialAndWriteFunction(t *testing.T) {
    frame := fixture(t, "read_holding_response.hex")
    if _, err := ParseReadResponse(frame, 1, 7); err == nil { t.Fatal("expected serial mismatch") }
    frame[len(frame)-8] = 0x06
    if _, err := ParseReadResponse(frame, 123456789, 7); err == nil { t.Fatal("expected unsupported function") }
}
```

- [ ] **Step 2: Confirm tests fail before implementation**

Run: `go test ./internal/solarman -run 'Test(CRC|Build|Parse)'`

Expected: FAIL with undefined `CRC16`, `BuildReadRequest`, and `ParseReadResponse`.

- [ ] **Step 3: Implement CRC, strict frame builder, and parser**

Use these frame rules in `frame.go`: start bytes `0xA5 0x17`, little-endian payload length, control `0x45`, sequence, logger serial, Modbus RTU payload, additive frame checksum, end byte `0x15`. Reject bad markers, length, checksum, serial, sequence, Modbus CRC, exception response, non-`0x03` function, odd byte count, count above 125, and trailing bytes. Return typed sentinel errors `ErrMalformedFrame`, `ErrIdentityMismatch`, `ErrUnsupportedFunction`, `ErrModbusException`, and `ErrCRC` so transport tests can distinguish retryable corruption from configuration mismatch.

`CRC16` must use Modbus polynomial `0xA001`, initial value `0xFFFF`, and return low-byte-first wire value represented as `uint16`. `BuildReadRequest` must return an error when `count == 0 || count > 125`; no generic function-code argument may exist.

- [ ] **Step 4: Run protocol tests plus fuzz seed**

Add:

```go
func FuzzParseReadResponse(f *testing.F) {
    raw, err := os.ReadFile("testdata/read_holding_response.hex")
    if err != nil { f.Fatal(err) }
    seed, err := hex.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
    if err != nil { f.Fatal(err) }
    f.Add(seed)
    f.Fuzz(func(t *testing.T, b []byte) { _, _ = ParseReadResponse(b, 123456789, 7) })
}
```

Run: `go test ./internal/solarman && go test -fuzz=FuzzParseReadResponse -fuzztime=5s ./internal/solarman`

Expected: unit tests PASS; fuzz run reports no panic.

- [ ] **Step 5: Commit framing**

```bash
git add internal/solarman
git commit -m "feat: add read-only Solarman V5 framing"
```

### Task 3: Serialized TCP Client and Recovery

**Files:**
- Create: `internal/solarman/transport.go`, `transport_test.go`
- Create: `internal/solarman/client.go`, `client_test.go`

**Interfaces:**
- Consumes: `BuildReadRequest` and `ParseReadResponse` from Task 2.
- Produces: `type RegisterReader interface { ReadHoldingRegisters(context.Context, byte, uint16, uint16) ([]uint16, error) }`; `NewClient(Config, Dialer) *Client`; `(*Client).ReadHoldingRegisters(...)`.

- [ ] **Step 1: Write concurrency and reconnect tests**

```go
type fakeConn struct { mu sync.Mutex; active, max int; responses [][]byte; writes int }

func (f *fakeConn) RoundTrip(_ context.Context, request []byte) ([]byte, error) {
    f.mu.Lock(); f.active++; if f.active > f.max { f.max = f.active }; f.writes++; n := f.writes; f.mu.Unlock()
    defer func(){ f.mu.Lock(); f.active--; f.mu.Unlock() }()
    if n == 1 { return nil, io.EOF }
    return append([]byte(nil), f.responses[0]...), nil
}

func TestClientSerializesAndReconnectsOnce(t *testing.T) {
    conn := &fakeConn{responses:[][]byte{fixture(t,"read_holding_response.hex")}}
    client := NewClient(Config{Serial:123456789, Timeout:time.Second}, func(context.Context)(RoundTripper,error){ return conn,nil })
    var wg sync.WaitGroup
    for range 2 { wg.Add(1); go func(){ defer wg.Done(); _, _ = client.ReadHoldingRegisters(context.Background(),1,0,10) }() }
    wg.Wait()
    if conn.max != 1 { t.Fatalf("max concurrent requests = %d", conn.max) }
    if conn.writes < 2 { t.Fatalf("expected reconnect retry, writes=%d", conn.writes) }
}
```

- [ ] **Step 2: Confirm missing client failure**

Run: `go test ./internal/solarman -run TestClient`

Expected: FAIL with undefined `NewClient`, `Config`, `RoundTripper`.

- [ ] **Step 3: Implement bounded serialized client**

Define:

```go
type RoundTripper interface { RoundTrip(context.Context, []byte) ([]byte, error) }
type Dialer func(context.Context) (RoundTripper, error)
type Config struct { Address string; Serial uint32; Timeout time.Duration }
```

`Client` owns `sync.Mutex`, sequence counter, current connection, and dialer. Each read locks across dial/request/response, derives a timeout context, increments nonzero sequence, retries exactly once after I/O/CRC/malformed-frame errors, and never retries `ErrIdentityMismatch`, `ErrUnsupportedFunction`, or Modbus exception. TCP implementation uses `net.Dialer.DialContext`, `SetDeadline`, `io.ReadFull` for fixed header then declared payload, maximum frame size 512 bytes, and closes connection after any partial read.

- [ ] **Step 4: Run race detector**

Run: `go test -race ./internal/solarman`

Expected: PASS and no race report.

- [ ] **Step 5: Commit transport**

```bash
git add internal/solarman
git commit -m "feat: add serialized Solarman TCP client"
```

### Task 4: Typed SOFAR Snapshot Decoder

**Files:**
- Create: `internal/domain/telemetry.go`
- Create: `internal/sofar/registers.go`, `decoder.go`, `decoder_test.go`
- Create: `internal/sofar/testdata/normal_day.json`, `pv2_inactive.json`, `fault.json`

**Interfaces:**
- Consumes: `solarman.RegisterReader.ReadHoldingRegisters`.
- Produces: `sofar.Reader.ReadSnapshot(context.Context) (domain.TelemetrySnapshot, error)` and stable `domain.TelemetrySnapshot` fields used by all later plans.

- [ ] **Step 1: Define fixture-driven domain expectations**

```go
package domain

import "time"

type MPPT struct { Active bool `json:"active"`; VoltageV, CurrentA, PowerW float64 }
type Grid struct { VoltageV, FrequencyHz float64 }
type TelemetrySnapshot struct {
    ObservedAt time.Time `json:"observedAt"`
    Status string `json:"status"`
    ACPowerW, EnergyTodayWh, EnergyLifetimeWh float64
    PV1, PV2 MPPT
    Grid Grid
    FaultCodes []uint16 `json:"faultCodes"`
}
```

Test table loads sanitized JSON containing raw register arrays and expected snapshot. Assertions must use `math.Abs(got-want) <= 0.01`, verify normal status, fault codes, energy scaling, PV1 values, grid values, and `PV2.Active == false` for zeroed inactive fixture.

- [ ] **Step 2: Confirm decoder test fails**

Run: `go test ./internal/sofar`

Expected: FAIL because `NewReader` and `ReadSnapshot` do not exist.

- [ ] **Step 3: Implement explicit register map and decoder**

Define immutable read blocks with start/count and named offsets. `ReadSnapshot` reads blocks in ascending address order through `RegisterReader`; converts unsigned/signed 16-bit fields explicitly; joins 32-bit energy counters with documented word order; applies per-register scale; validates voltage `0..600 V`, current `0..30 A`, frequency `40..70 Hz`, AC power `0..6600 W`, monotonic nonnegative energy; and returns field-specific errors. `ReaderConfig` contains `SlaveID byte`, `ActiveMPPT map[int]bool`, and injected `Now func() time.Time`. Inactive MPPT values are preserved but `Active` remains false; no error follows from zero values.

- [ ] **Step 4: Run decoder and entire backend tests**

Run: `go test ./internal/sofar ./internal/solarman ./internal/httpserver && go vet ./...`

Expected: PASS; `go vet` exits 0.

- [ ] **Step 5: Commit decoder**

```bash
git add internal/domain internal/sofar
git commit -m "feat: decode SOFAR telemetry snapshots"
```

### Task 5: Explicit Read-Only Hardware Probe

**Files:**
- Create: `cmd/helio-hardware-test/main.go`
- Create: `internal/sofar/hardware_test.go`
- Modify: `Makefile`
- Modify: `.gitignore`
- Create: `.env.hardware.example`

**Interfaces:**
- Consumes: Solarman client and SOFAR reader from Tasks 3–4.
- Produces: `make hardware-test`, which prints one redacted JSON snapshot and never writes registers.

- [ ] **Step 1: Add opt-in guard test**

```go
func TestHardwareReadOnly(t *testing.T) {
    if os.Getenv("HELIO_HARDWARE_TEST") != "1" { t.Skip("set HELIO_HARDWARE_TEST=1") }
    cfg, err := hardwareConfigFromEnv()
    if err != nil { t.Fatal(err) }
    snapshot, err := newHardwareReader(cfg).ReadSnapshot(context.Background())
    if err != nil { t.Fatal(err) }
    if snapshot.ObservedAt.IsZero() { t.Fatal("missing observation time") }
}
```

- [ ] **Step 2: Verify default run skips**

Run: `go test -v ./internal/sofar -run TestHardwareReadOnly`

Expected: PASS with `SKIP` and no network connection.

- [ ] **Step 3: Implement env parsing and redacted probe**

Require `HELIO_LOGGER_IP`, `HELIO_LOGGER_SERIAL`, `HELIO_MODBUS_SLAVE`, and optional `HELIO_LOGGER_PORT=8899`. Reject hostname containing URL schemes, non-private IP by default, serial outside uint32, slave outside `1..247`, and any port outside `1..65535`. Permit non-private targets only with `HELIO_ALLOW_NON_PRIVATE_LOGGER=1`. Probe output contains telemetry only; never address, serial, credentials, or raw frames. Ignore `.env.hardware`; example contains documentation keys with blank values.

- [ ] **Step 4: Run offline and authorized live checks**

Run offline: `go test ./...`

Expected: PASS without hardware.

Run live only on owner LAN: `set -a; source .env.hardware; set +a; HELIO_HARDWARE_TEST=1 go test -v ./internal/sofar -run TestHardwareReadOnly`

Expected: PASS with one valid snapshot; no source or fixture file changes.

- [ ] **Step 5: Commit safe probe**

```bash
git add cmd/helio-hardware-test internal/sofar/hardware_test.go Makefile .gitignore .env.hardware.example
git commit -m "test: add opt-in read-only hardware probe"
```

## Plan Acceptance

Run:

```bash
go test -race ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
go build ./cmd/helio ./cmd/helio-hardware-test
git grep -E '192\.168\.|[0-9]{10}' -- ':!package-lock.json' ':!docs/superpowers/plans/*'
```

Expected: tests/builds PASS; final grep returns no deployment identifier. Start `go run ./cmd/helio`, verify `/health/live`, `/health/ready`, and `/history` return 200.
