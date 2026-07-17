# Task 5 report: authenticated finance API

Completed authenticated finance endpoints:

- `GET /api/v1/finance/summary`
- `GET` and CSRF-protected `POST /api/v1/finance/cycles`
- `GET /api/v1/finance/tariff-proposals`
- CSRF-protected `POST /api/v1/finance/tariff-proposals/{id}/approve`

The cycle DTO uses pointers, strict JSON decoding, RFC3339 timestamps, and integer-only kWh/centavo fields. Invalid, missing, unknown, fractional, negative, or reversed cycle data returns `422 invalid_finance_cycle`. Finance mutations run as the session principal; repository transactions record the existing billing and tariff approval audit entries. API responses expose camel-case DTOs, RFC3339 timestamps, integer monetary amounts, and inherit `Cache-Control: no-store`.

Added finance repository proposal listing and wired it into the application. Focused HTTP tests cover CSRF, projection response, and audit evidence.

Verification: `rtk go test ./internal/api ./internal/app -count=1` (53 passed).

## Review follow-up

The billing-cycle decoder now uses `http.MaxBytesReader` and preserves
`*http.MaxBytesError` through both JSON decoding passes, including trailing
whitespace after otherwise-valid JSON. Oversized requests return `413
request_too_large`.

Added table-driven HTTP regression coverage for absent and unknown fields,
negative and fractional kWh/centavo values, reversed dates, and non-RFC3339
dates. Every case returns `422 invalid_finance_cycle`.

Verification: `rtk go test ./internal/api -run 'TestCreateCycleRejectsInvalidInput|TestCreateCycleRejectsBodyOverLimitIncludingTrailingWhitespace' -count=1` (10 passed); `rtk go test ./internal/api ./internal/app -count=1` (63 passed).
