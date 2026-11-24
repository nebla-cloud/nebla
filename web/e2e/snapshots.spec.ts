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
import { adminState, createServer, expectNoLeaks, serverHeader } from './helpers'

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

test('take, build from, restore and delete a snapshot', async ({ page }) => {
  const snap = 'e2e-snap'
  const nav = page.getByRole('complementary', { name: 'Project menu' })
  const tabs = page.getByRole('navigation', { name: 'Server sections' })

  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  const source = await createServer(page)

  await tabs.getByRole('link', { name: 'Snapshots' }).click()
  await expect(page.getByText('No snapshots of this server')).toBeVisible()
  await page.getByRole('button', { name: 'Take snapshot' }).click()
  const take = page.getByRole('dialog')
  await expect(
    take.getByText(
      `${source} is running. Nebla will shut it down, save its disk and start it again. Expect a few minutes offline.`,
    ),
  ).toBeVisible()
  await expectNoLeaks(page)
  await take.locator('input[name=name]').fill(snap)
  await take.getByRole('button', { name: 'Take snapshot' }).click()
  await expect(take).toBeHidden()
  await expect(page.getByText(`${snap} saved`, { exact: true })).toBeVisible({ timeout: 90_000 })
  const card = page.getByTestId('server-snapshot').filter({ hasText: snap })
  await expect(card).toBeVisible()
  await expectNoLeaks(page)

  await card.getByRole('button', { name: `Restore ${snap}` }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Restore', exact: true }).click()
  await expect(page.getByText(`${source} rebuilt`, { exact: true })).toBeVisible({
    timeout: 90_000,
  })
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 90_000,
  })

  await nav.getByRole('link', { name: 'Snapshots', exact: true }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/snapshots$/)
  const row = page.getByRole('link', { name: snap })
  await expect(row).toBeVisible()
  await expect(page.getByText(source, { exact: true })).toBeVisible()
  await expectNoLeaks(page)

  await nav.getByRole('link', { name: 'Servers', exact: true }).click()
  await page.getByRole('link', { name: 'Create server' }).first().click()
  await expect(page.getByRole('heading', { name: 'Create server' })).toBeVisible()
  await page.locator('input[name=snapshot]').first().check({ force: true })
  await expect(page.getByRole('definition').filter({ hasText: snap })).toBeVisible()
  const create = page.getByRole('button', { name: 'Create server' })
  await expect(create).toBeEnabled()
  await expectNoLeaks(page)
  await create.click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers\/\d+$/)
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 90_000,
  })

  await nav.getByRole('link', { name: 'Snapshots', exact: true }).click()
  await page.getByRole('button', { name: `Restore ${snap}` }).click()
  const restore = page.getByRole('dialog')
  await expect(restore.getByText(new RegExp(`^Restore ${source} to ${snap}\\?`))).toBeVisible()
  await expect(restore.getByText(/Volumes stay attached/)).toBeVisible()
  await restore.getByRole('button', { name: 'Restore', exact: true }).click()
  await expect(restore).toBeHidden()
  await expect(page.getByText(`${source} rebuilt`, { exact: true })).toBeVisible({
    timeout: 90_000,
  })
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 90_000,
  })
  await expectNoLeaks(page)

  await nav.getByRole('link', { name: 'Snapshots', exact: true }).click()
  await page.getByRole('link', { name: snap }).click()
  await expect(page.getByRole('heading', { level: 1, name: snap })).toBeVisible()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(snap)
  await confirm.getByRole('button', { name: 'Delete snapshot' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/snapshots$/, { timeout: 60_000 })
  await expect(page.getByText('No snapshots yet')).toBeVisible({ timeout: 60_000 })
  await expectNoLeaks(page)
})
