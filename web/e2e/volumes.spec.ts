import { expect, test } from '@playwright/test'
import { adminState, expectNoLeaks } from './helpers'

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

test('create, attach, detach and delete a volume', async ({ page }) => {
  const vol = 'e2e-data'
  const nav = page.getByRole('complementary', { name: 'Project menu' })

  // server-depth leaves one server in Home lab; the volume attaches to it.
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)

  await nav.getByRole('link', { name: 'Volumes', exact: true }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/volumes$/)
  await expect(page.getByText('No volumes yet')).toBeVisible()
  await expectNoLeaks(page)

  await page.getByRole('button', { name: 'Create volume' }).click()
  const form = page.getByRole('dialog')
  await form.locator('input[name=name]').fill(vol)
  await form.locator('input[name=size]').fill('20')
  const pick = form.locator('select[name=server]')
  const target = pick.locator('option:not([disabled])').nth(1)
  const serverName = (await target.textContent())!.trim()
  await pick.selectOption({ label: serverName })
  await expectNoLeaks(page)
  await form.getByRole('button', { name: 'Create volume' }).click()
  await expect(form).toBeHidden()

  const row = page.getByRole('link', { name: new RegExp(vol) })
  await expect(row).toContainText('20 GB')
  await expect(row).toContainText(serverName, { timeout: 60_000 })
  await expectNoLeaks(page)

  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await page.getByRole('link', { name: new RegExp(serverName) }).click()
  await expect(page.getByRole('heading', { level: 1, name: serverName })).toBeVisible()
  await page
    .getByRole('navigation', { name: 'Server sections' })
    .getByRole('link', { name: 'Volumes' })
    .click()
  const card = page.getByTestId('server-volume').filter({ hasText: vol })
  await expect(card).toContainText('Attached')
  await expect(card).toContainText('20 GB')
  await expect(card.getByTestId('linux-device')).toHaveText(
    /^\/dev\/disk\/by-id\/virtio-nb-vol-\d+$/,
  )
  await expectNoLeaks(page)

  await card.getByRole('button', { name: `Detach ${vol}` }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Detach' }).click()
  await expect(page.getByText(`${vol} detached`, { exact: true })).toBeVisible({ timeout: 60_000 })
  await expect(page.getByText('No volumes attached')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Attach volume' })).toBeVisible()
  await expectNoLeaks(page)

  await nav.getByRole('link', { name: 'Volumes', exact: true }).click()
  await page.getByRole('link', { name: new RegExp(vol) }).click()
  await expect(page.getByRole('heading', { level: 1, name: vol })).toBeVisible()
  await expect(page.getByRole('definition').filter({ hasText: 'Not attached' })).toBeVisible()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(vol)
  await confirm.getByRole('button', { name: 'Delete volume' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/volumes$/, { timeout: 60_000 })
  await expect(page.getByText(`${vol} deleted`, { exact: true })).toBeVisible()
  await expect(page.getByText('No volumes yet')).toBeVisible({ timeout: 60_000 })
  await expectNoLeaks(page)
})
