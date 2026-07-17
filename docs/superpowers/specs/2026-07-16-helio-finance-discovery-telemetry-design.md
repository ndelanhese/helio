# Helio Finance, Discovery, and Extended Telemetry Design

Date: 2026-07-16

## Purpose

Extend Helio from inverter production monitoring into a local financial and system-health product. The first release covers one generating consumer unit (UC), supports accurate monthly bill reconciliation, reduces setup friction, and accepts an optional grid meter without making one mandatory.

This design preserves the existing local-first, read-only inverter boundary. It does not parse bills automatically, manage beneficiary UCs, control the inverter, or require a physical weather station.

## Product Outcomes

- Connect a Solarman V5 logger after serial entry or QR scan, then discover its IP on an explicitly approved local subnet.
- Calculate and explain a projected bill using an approved tariff schedule, current inverter generation, and recorded billing history.
- Reconcile a projected bill against six manually entered fields from each completed bill.
- Track credit balance and expiring credit lots for the generating UC.
- Distinguish measured, invoiced, modeled, and estimated values everywhere.
- Improve system-health insights with inverter temperature when a verified device profile supports it, and with existing virtual weather data.

## Scope and Boundaries

### Single generating UC

The UI and financial engine operate on one generating UC. Domain identifiers must not prevent a future many-UC model, but no beneficiary UC, allocation, transfer, or cross-UC credit calculation is in scope.

### Meter modes

Helio must work in both modes:

| Mode | Inputs | Results |
| --- | --- | --- |
| No grid meter | Inverter telemetry, approved tariff, monthly bill record | Production, projected bill, historical reconciliation, credit balance and expiry forecast. |
| Bidirectional grid meter | Above inputs plus import/export power and energy | Real-time import, export, home consumption, self-consumption, and a more accurate within-cycle projection. |

Without a meter, Helio must never claim real-time house consumption, self-consumption, or export. It must label bill projections as estimates until reconciled with a bill.

With a meter, direct self-consumption is `inverter generation - measured export` only when no battery or other generator is configured. Those unsupported topologies suppress the derived metric instead of estimating it.

## Financial Model

### Tariff sources and approval

Helio obtains tariff candidates from official distributor sources and monthly flag candidates from ANEEL. The first source adapter targets Copel Group B. It records source URL, retrieval time, tariff fields, effective date range, and parser version.

A discovered tariff is a proposal. It cannot affect calculations until the user approves it. An approved tariff version is immutable, including after a future tariff refresh. A changed official tariff creates a new pending proposal; it never rewrites history.

If a source fails or its result cannot be validated, Helio retains the last approved tariff, marks it stale, and offers manual entry. Source fetches contain no UC number, customer identity, bill contents, or inverter data.

### Tariff schedule

The schedule stores the component rates and applicability needed to explain a calculation:

- distributor, group, class, subclass, tariff modality, municipality, and effective dates;
- consumption TE and TUSD;
- compensation TE/TUSD components applicable to the configured GD modality;
- monthly tariff flag and its charge;
- configured phase count and availability floor: 30, 50, or 100 kWh;
- tax calibration fields from a reconciled bill; and
- fixed local charges, including CIP, as separate non-solar items.

The installation setup records GD modality and connection date. If these cannot be determined reliably, Helio asks for manual confirmation and marks compensation calculations as lower confidence.

PIS/Cofins and some tax treatment can vary by billing cycle. Official rates are a starting point; reconciliation calibrates the effective monthly values. CIP, service charges, interest, fines, instalments, and unrelated adjustments remain separate and are excluded from solar savings.

### Billing-cycle entry

One completed bill is entered with six required fields:

1. Reading start and end dates.
2. Active consumption in kWh.
3. Energy injected in kWh.
4. Credits used in kWh.
5. Final credit balance in kWh.
6. Total amount paid.

The cycle period, rather than a reference month label, is authoritative. Helio associates its telemetry and tariff version to that period. It stores the calculated projection, actual bill record, confidence, variance, and calculation breakdown.

Credit lots store origin cycle, available kWh, expiry date, and consumption order. When only aggregate balance and the next known expiry are available, Helio records a partial lot state and labels the expiry forecast accordingly. It must not fabricate a full expiry schedule.

### Financial views

The product provides:

- a next-bill projection with separate TE, TUSD, compensation, flag, taxes, availability floor, and CIP rows;
- a same-cycle counterfactual called "without solar compensation" that holds taxes, flag, and non-solar fixed charges constant while removing compensation credits;
- actual versus projected bill history, with variance explanation;
- credit balance, conservative value estimate, next known expiry, and estimated months of coverage; and
- generation and credits as separate series. Inverter generation is not treated as billed injection.

## Setup Assistant

### Location

The browser offers device geolocation after an explicit permission request. It submits latitude, longitude, and accuracy to Helio, which shows the result for confirmation before saving. Rejection or failure presents city, CEP, or coordinate entry. IP geolocation is excluded for accuracy and privacy reasons.

### Logger discovery

1. User enters a logger serial or reads a QR/label using the device camera.
2. The QR parser extracts a numeric serial from plain text or a supported URL, then displays an editable value for confirmation.
3. Helio proposes a private CIDR; the user explicitly confirms the scan range.
4. Backend scanner attempts TCP connections to port 8899 with strict timeouts, a bounded worker pool, and a default maximum `/24` range.
5. Each open candidate is validated with the supplied serial, configured/default Modbus slave, and a read-only Solarman request.
6. Helio displays confirmed IP, serial, detected model/profile, and test result. User confirmation saves configuration.

The browser does not scan the network. The Go backend does. Broad scans such as `/16` require an explicit advanced action; scans operate only on private addresses. No discovery flow can issue Modbus writes.

Docker networking can reach the LAN while still exposing only a container subnet to the process. The assistant must therefore permit manual CIDR entry and retain IP entry as a permanent fallback. VLANs, firewall rules, and Wi-Fi client isolation are surfaced as connection guidance, not silently retried.

Solarman V5 read frames require the logger serial to validate identity, so fully automatic serial discovery is not promised.

## Telemetry Expansion

### Inverter profile data

SOFAR data collection gains model-specific read-only register profiles. A profile may expose inverter temperature, additional status, or additional electrical measurements only after captured-frame and hardware validation. Unknown registers are not probed in normal collection.

Inverter temperature has source `inverter`. It is not labeled as ambient or panel temperature.

### Weather and station abstraction

Open-Meteo remains the default virtual weather provider for ambient temperature, clouds, wind, precipitation, and modeled irradiance. Its data supports expectation and confidence, not a claim of on-site irradiance.

A future local-station provider follows the same source contract. It can add measured ambient temperature, module temperature, irradiance, wind, or precipitation. Each observation records source and freshness. Missing station hardware never degrades inverter collection.

### Optional meter abstraction

`MeterReader` supplies normalized import/export power, cumulative imported/exported energy, optional phase values, observed time, and freshness. First integrations may use local Modbus TCP or RS-485 devices; vendor support is selected only after a hardware validation profile exists.

The meter must be installed at the service entrance and measure every phase. A meter connected only to the inverter cannot provide household import/export flow.

## Architecture

New internal boundaries:

- `discovery`: QR parsing, range validation, port probing, and read-only identity verification.
- `tariffs`: official source adapters, parser validation, versioning, proposals, approval, and manual fallback.
- `billing`: cycles, bill records, reconciliation, and credit lots.
- `finance`: tariff application, projection, counterfactuals, confidence, and explanations.
- `meter`: normalized meter interface, collector adapter, and optional samples.
- `sofar`: verified model register profiles and decoding.
- `weather`: existing virtual provider plus future station-provider interface.

SQLite additions:

- `tariff_versions`
- `tariff_proposals`
- `billing_cycles`
- `credit_lots`
- `bill_reconciliations`
- `meter_samples`
- `discovery_audit`

`meter_samples` remains empty without a configured meter. `discovery_audit` contains only scan scope, timing, outcome counts, and selected identity; it does not retain arbitrary host inventories.

## Failure Handling and Privacy

- Tariff source failure leaves local monitoring live and uses the last approved schedule.
- Candidate parsing failure creates no proposal and shows a manual tariff path.
- Discovery timeout, malformed response, or serial mismatch never changes logger configuration.
- Meter failure changes health/freshness state and falls back to no-meter financial mode; it never blocks inverter collection.
- Weather failure retains cached weather and lowers confidence.
- Source labels, freshness, tariff version, and confidence accompany every calculated result.

No inverter write endpoint, scan write operation, or public-network scan is introduced. Official tariff lookup sends no customer-specific information.

## Delivery Order

1. Financial persistence, tariff-version proposal/approval, manual bill entry, credit lots, calculation engine, and reconciliation views.
2. Copel and ANEEL official-source adapters with source validation, cache, staleness, and manual fallback.
3. QR/serial onboarding and bounded LAN IP discovery.
4. Validated SOFAR profile expansion, including inverter temperature where supported.
5. Optional bidirectional meter interface and first validated hardware profile.
6. Local weather-station provider.

## Testing

- Unit tests cover tariff selection, effective-date boundaries, availability floors, compensation components, tax calibration, counterfactuals, credit-lot depletion, expiry forecasts, and no-meter labels.
- Source adapters use recorded official fixtures; changed schema and invalid values fail closed into a pending/manual state.
- Discovery tests cover CIDR validation, private-address enforcement, worker limits, timeouts, serial mismatch, and proof that only read functions are called.
- Fake meter and inverter tests cover availability transitions and ensure no-meter mode remains functional.
- SOFAR profile fixtures validate every added register and scaling rule.
- API, migration, and UI tests cover proposal approval, manual cycle entry, source attribution, stale tariff behavior, QR correction, scan consent, and financial explanations.

## Official References

- Copel tariff page: https://www.copel.com/site/copel-distribuicao/tarifas-de-energia-eletrica/
- ANEEL tariff flags: https://www.gov.br/aneel/pt-br/assuntos/tarifas/bandeiras-tarifarias
- ANEEL July 2026 flag announcement: https://www.gov.br/aneel/pt-br/assuntos/noticias/2026-defeso-eleitoral/bandeira-tarifaria-em-julho-permanece-amarela/
