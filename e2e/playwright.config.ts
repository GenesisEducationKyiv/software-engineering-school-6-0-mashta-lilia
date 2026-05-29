import { defineConfig, devices } from '@playwright/test';

// Read the base URL from env so CI can override it; defaults to the host
// port that docker-compose.e2e.yml maps for the app service.
const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost:8081';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,           // tests share a Postgres row state — keep them serial
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
