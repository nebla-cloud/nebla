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

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

const PANEL =
  'New and rebuilt servers set this up automatically. For a running server, run these commands.'
// Members never see these.
const HIDDEN = /vnet|nb000\d|nebula|\bzone\b|\bsdn\b|bridge/i

test('create a network, join two servers, detach and delete', async ({ page }) => {
  test.setTimeout(300_000)
  const name = 'backend'
  const nav = page.getByRole('complementary', { name: 'Project menu' })
  const noHidden = async () => expect(await page.content()).not.toMatch(HIDDEN)

  // The admin points the location at the fake cluster's zone.
  await page.goto('/admin/locations')
  await page.getByRole('link', { name: /home/ }).first().click()
  const card = page.getByTestId('private-networks')
  await expect(card).toContainText('Private networks are off at this location.')
  await card.locator('input[name=sdn_zone]').fill('nebula')
  await card.getByRole('button', { name: 'Save' }).click()
  await expect(card).toContainText('2 available', { timeout: 30_000 })
  await expect(card).not.toContainText('Isolation off')
  await expect(card).not.toContainText('SDN.Audit')

  // server-depth and firewalls each leave a running server in Home lab.
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)

  await nav.getByRole('link', { name: 'Networks', exact: true }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/networks$/)
  await expect(page.getByText('No networks yet')).toBeVisible()
  await expectNoLeaks(page)
  await noHidden()

  await page.getByRole('button', { name: 'Create network' }).first().click()
  const create = page.getByRole('dialog')
  await create.locator('input[name=name]').fill(name)
  await create.locator('select[name=location]').selectOption({ label: 'home' })
  await create.getByRole('button', { name: 'Create network' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/networks\/\d+$/)
  const netPath = new URL(page.url()).pathname
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible()
  const range = (await page.locator('dt:text-is("Range") + dd').textContent())!.trim()
  expect(range).toMatch(/^10\.\d+\.0\.0\/16$/)
  await expect(page.getByText('No servers on this network yet.')).toBeVisible()
  await expectNoLeaks(page)
  await noHidden()

  // Create a server that joins the network.
  await nav.getByRole('link', { name: 'Servers', exact: true }).click()
  await page.getByRole('link', { name: 'Create server' }).first().click()
  await expect(page.getByRole('heading', { name: 'Create server' })).toBeVisible()
  await page.locator('label').filter({ hasText: name }).first().click()
  const submit = page.getByRole('button', { name: 'Create server' })
  await expect(submit).toBeEnabled()
  await expectNoLeaks(page)
  await noHidden()
  await submit.click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers\/\d+$/)
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 90_000,
  })
  const created = (await page.getByRole('heading', { level: 1 }).textContent())!.trim()

  await page
    .getByRole('navigation', { name: 'Server sections' })
    .getByRole('link', { name: 'Networking' })
    .click()
  const row = page.getByRole('row').filter({ hasText: name })
  await expect(row).toBeVisible()
  await expect(row).toContainText(/^.*10\.\d+\.0\.\d+/)
  await expectNoLeaks(page)
  await noHidden()

  // Attach a second server from the network page.
  await page.goto(netPath)
  await expect(page.getByRole('row').filter({ hasText: created })).toBeVisible()
  await page.getByRole('button', { name: 'Attach server' }).click()
  const attach = page.getByRole('dialog')
  const pick = attach.locator('select[name=server]')
  const second = (await pick.locator('option:not([disabled])').nth(1).textContent())!.trim()
  expect(second).not.toBe(created)
  await pick.selectOption({ label: second })
  await attach.getByRole('button', { name: 'Attach', exact: true }).click()

  const guest = page.getByRole('dialog', { name: 'Run this on the server' })
  await expect(guest).toBeVisible({ timeout: 60_000 })
  await expect(guest).toContainText(PANEL)
  await expect(guest).toContainText(second)
  await expect(guest.getByTestId('guest-apply')).toBeVisible()
  // These servers have net_refresh: cloud-init writes the NIC at the next boot, so there is nothing to persist.
  await expect(guest.getByTestId('guest-persist')).toHaveCount(0)
  await noHidden()
  await guest.getByRole('button', { name: 'Done' }).click()
  await expect(guest).toBeHidden()
  await expect(page.getByRole('row').filter({ hasText: second })).toBeVisible({ timeout: 60_000 })

  // Delete waits until both servers have left.
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
  for (const server of [second, created]) {
    const attached = page.getByRole('row').filter({ hasText: server })
    await attached.getByRole('button', { name: 'Detach' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Detach', exact: true }).click()
    // With net_refresh there is no persisted file to remove, so no commands follow a detach.
    await expect(attached).toBeHidden({ timeout: 60_000 })
    await expect(guest).toBeHidden()
  }
  await expect(page.getByText('No servers on this network yet.')).toBeVisible()

  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toBeEnabled({
    timeout: 60_000,
  })
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(name)
  await confirm.getByRole('button', { name: 'Delete network' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/networks$/, { timeout: 60_000 })
  await expect(page.getByText(`${name} deleted`, { exact: true })).toBeVisible()
  await expect(page.getByText('No networks yet')).toBeVisible({ timeout: 60_000 })
  await expectNoLeaks(page)
  await noHidden()
})
