import { expect, test } from '@playwright/test'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as { fail_checksum: string }

test('failed build can be retried', async ({ page, request }) => {
  await page.goto('/login')
  await page.locator('input[name=email]').fill('admin@example.test')
  await page.locator('input[name=password]').fill('correct-horse-battery')
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).not.toHaveURL(/\/login/)

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
