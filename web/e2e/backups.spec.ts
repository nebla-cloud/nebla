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

import { expect, test } from '@playwright/test'
import { adminState, expectNoLeaks, serverHeader } from './helpers'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as { enable_backups: string }

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

test('enable backups, back up now, restore and delete', async ({ page }) => {
  // The harness points its locations at a PBS storage; the panel offers backups once it does.
  const armed = await page.request.post(harness.enable_backups)
  expect(armed.status()).toBe(204)

  // An earlier spec leaves a server in Home lab.
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await page.getByRole('link', { name: /^(?!Create)/ }).filter({ hasText: 'Running' }).first().click()
  await expect(page).toHaveURL(/\/servers\/\d+$/)
  const title = page.getByRole('heading', { level: 1 })
  await expect(title).not.toHaveText('Servers')
  const name = (await title.textContent())!.trim()
  await page
    .getByRole('navigation', { name: 'Server sections' })
    .getByRole('link', { name: 'Backups' })
    .click()
  await expect(page).toHaveURL(/\/servers\/\d+\/backups$/)
  await expect(page.getByText('No backups yet')).toBeVisible()
  await expectNoLeaks(page)

  await page.getByRole('switch', { name: 'Daily backups' }).click()
  await expect(page.getByText('Next backup:')).toBeVisible()

  await page.getByRole('button', { name: 'Back up now' }).click()
  await expect(page.getByText('A backup is already waiting or running')).toBeVisible()
  const row = page.getByTestId('backup').first()
  await expect(row.getByRole('button', { name: /^Restore/ })).toBeVisible({ timeout: 90_000 })
  await expect(row).toContainText('Manual')
  await expect(page.getByText('A backup is already waiting or running')).toBeHidden()
  await expect(page.getByText('Last backup:')).toBeVisible()
  await expectNoLeaks(page)

  await row.getByRole('button', { name: /^Restore/ }).click()
  const restore = page.getByRole('dialog')
  await expect(restore).toContainText(`Restore ${name} to`)
  await expect(restore).toContainText('The current disk will be replaced.')
  await restore.locator('input[name=confirm_name]').fill(name)
  await restore.getByRole('button', { name: 'Restore server' }).click()
  await expect(restore).toBeHidden()
  await expect(page.getByText(`${name} restored`, { exact: true })).toBeVisible({
    timeout: 120_000,
  })
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expectNoLeaks(page)

  await page.getByTestId('backup').first().getByRole('button', { name: /^Delete/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete backup' }).click()
  await expect(page.getByText('No backups yet')).toBeVisible({ timeout: 90_000 })
  await expectNoLeaks(page)
})
