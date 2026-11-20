import { expect, test } from '@playwright/test'
import { adminState } from './helpers'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as { fail_checksum: string }

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

test('failed build can be retried', async ({ page, request }) => {
  await page.goto('/admin/os-images')
  const cell = (state: string) =>
    page.getByRole('button', { name: `Debian 13 in home: ${state}. Show details` })

  // The first run left it ready, so turn it off to enable it afresh.
  await cell('Ready').click()
  await page.getByRole('dialog').getByRole('button', { name: 'Disable' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Disable' }).click()
  await expect(page.getByRole('button', { name: 'Enable Debian 13 in home' })).toBeVisible({
    timeout: 60_000,
  })

  expect((await request.post(harness.fail_checksum)).status()).toBe(204)
  await page.getByRole('button', { name: 'Enable Debian 13 in home' }).click()
  await expect(cell('Failed')).toBeVisible({ timeout: 60_000 })

  await cell('Failed').click()
  const popover = page.getByRole('dialog')
  await expect(popover.getByText(/could not fetch the image checksum list/)).toBeVisible()
  await popover.getByRole('button', { name: 'Retry' }).click()
  await expect(cell('Ready')).toBeVisible({ timeout: 60_000 })
})
