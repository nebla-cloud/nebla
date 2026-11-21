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
    // One admin password login for the suite; the per-email login limiter allows ten.
    {
      name: 'admin-session',
      testMatch: /admin-session\.setup\.ts/,
      dependencies: ['first-run'],
    },
    { name: 'catalog', testMatch: /catalog\.spec\.ts/, dependencies: ['admin-session'] },
    {
      name: 'server-depth',
      testMatch: /server-depth\.spec\.ts/,
      dependencies: ['catalog'],
    },
    { name: 'firewalls', testMatch: /firewalls\.spec\.ts/, dependencies: ['server-depth'] },
    // Needs the Home lab project that first-run creates; runs last so it can't disturb server specs.
    { name: 'people', testMatch: /people\.spec\.ts/, dependencies: ['firewalls'] },
    { name: 'enroll', testMatch: /enroll\.spec\.ts/, dependencies: ['people'] },
    { name: 'passkeys', testMatch: /passkeys\.spec\.ts/, dependencies: ['enroll'] },
    { name: 'admin', testMatch: /admin\.spec\.ts/, dependencies: ['passkeys'] },
  ],
})
