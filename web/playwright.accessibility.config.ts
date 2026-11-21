import { defineConfig } from '@playwright/test'
import config from './playwright.config'

export default defineConfig({
  ...config,
  projects: [
    ...(config.projects ?? []).filter(
      ({ name }) => name === 'first-run' || name === 'admin-session',
    ),
    {
      name: 'accessibility',
      testMatch: /accessibility\.spec\.ts/,
      dependencies: ['admin-session'],
    },
  ],
})
