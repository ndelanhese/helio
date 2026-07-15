import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['line'], ['html', { open: 'never', outputFolder: 'test-results/playwright-report' }]],
  timeout: 60_000,
  use: {
    baseURL: 'http://127.0.0.1:4173',
    locale: 'pt-BR',
    reducedMotion: 'reduce',
    timezoneId: 'America/Sao_Paulo',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'desktop-chromium',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
    },
    {
      name: 'mobile-webkit',
      use: { ...devices['iPhone 13'], viewport: { width: 375, height: 812 } },
    },
  ],
  webServer: {
    command: 'npm run build && go run ../internal/fakeapp',
    cwd: '.',
    reuseExistingServer: false,
    timeout: 120_000,
    url: 'http://127.0.0.1:4173/health/live',
  },
})
