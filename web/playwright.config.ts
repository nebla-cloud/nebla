// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
    // Attaches a volume to the server server-depth leaves behind.
    { name: 'volumes', testMatch: /volumes\.spec\.ts/, dependencies: ['firewalls'] },
    // Backs up and restores the server server-depth leaves behind.
    { name: 'backups', testMatch: /backups\.spec\.ts/, dependencies: ['volumes'] },
    // Assigns and moves a floating IP between the servers server-depth and firewalls leave behind.
    {
      name: 'floating-ips',
      testMatch: /floating-ips\.spec\.ts/,
      dependencies: ['backups'],
    },
    // Needs the Home lab project that first-run creates; runs last so it can't disturb server specs.
    { name: 'people', testMatch: /people\.spec\.ts/, dependencies: ['floating-ips'] },
    { name: 'enroll', testMatch: /enroll\.spec\.ts/, dependencies: ['people'] },
    { name: 'passkeys', testMatch: /passkeys\.spec\.ts/, dependencies: ['enroll'] },
    { name: 'admin', testMatch: /admin\.spec\.ts/, dependencies: ['passkeys'] },
  ],
})
