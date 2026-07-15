# Browser acceptance suite

The fixture resets the deterministic fake server before every test and creates a fresh browser-context session. Specs must import `test` and `expect` from `./fixtures`; do not call the test-control endpoint directly or depend on state left by another test.

## Visual baselines

The four Chromium baselines are assertions, not attachments. Verify them with `npm run test:e2e`; intentionally refresh them with:

```sh
npm run test:e2e:update-snapshots
```

Review every changed PNG before committing it. A baseline update is acceptable only for an intentional UI change. Do not use `--update-snapshots` to make an unexplained failure disappear.

## Privacy review

Screenshots must never include passwords, session or CSRF tokens, real logger addresses or serial numbers, or personal installation data. The checked-in images use deterministic documentation-only values. After regenerating, inspect the images and run a strings scan for the fixture credentials and tokens before committing.
