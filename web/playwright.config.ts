import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  reporter: 'list',
  use: {
    baseURL: process.env.NEBLA_E2E_BASE_URL,
    trace: 'retain-on-failure',
    ...devices['Desktop Chrome'],
  },
  projects: [
    { name: 'first-run', testMatch: /first-run\.spec\.ts/ },
    { name: 'enroll', testMatch: /enroll\.spec\.ts/, dependencies: ['first-run'] },
    { name: 'catalog', testMatch: /catalog\.spec\.ts/, dependencies: ['first-run'] },
    {
      name: 'server-depth',
      testMatch: /server-depth\.spec\.ts/,
      dependencies: ['catalog'],
    },
    // Needs the Home lab project that first-run creates; runs last so it can't disturb server specs.
    { name: 'people', testMatch: /people\.spec\.ts/, dependencies: ['server-depth'] },
  ],
})
