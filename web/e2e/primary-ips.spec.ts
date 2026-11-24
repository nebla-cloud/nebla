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

import { expect, test, type Page } from '@playwright/test'
import { adminState, expectNoLeaks, serverHeader } from './helpers'

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

const NAME = 'e2e-keep'

// Creates a server on the form's defaults, with IPv4 taken from the named primary IP.
async function createWithIP(page: Page): Promise<string> {
  await page.getByRole('navigation').getByRole('link', { name: 'Servers' }).click()
  await page.getByRole('link', { name: 'Create server' }).first().click()
  await expect(page.getByRole('heading', { name: 'Create server' })).toBeVisible()
  const source = page.locator('select[name=ipv4_source]')
  const label = (await source.locator('option', { hasText: NAME }).textContent())!.trim()
  await source.selectOption({ label })
  await expect(page.getByRole('complementary', { name: 'Summary' })).toContainText(`IPv4 (${NAME})`)
  const create = page.getByRole('button', { name: 'Create server' })
  await expect(create).toBeEnabled()
  await expectNoLeaks(page)
  await create.click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers\/\d+$/)
  await expect(serverHeader(page).getByText('Running', { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  return (await page.getByRole('heading', { level: 1 }).textContent())!.trim()
}

test('reserve a primary IP, move it between servers, then delete it', async ({ page }) => {
  test.setTimeout(300_000)
  const nav = page.getByRole('complementary', { name: 'Project menu' })

  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)

  // Reserve an address that outlives its servers.
  await nav.getByRole('link', { name: 'Primary IPs', exact: true }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/primary-ips$/)
  await expectNoLeaks(page)
  await page.getByRole('button', { name: 'Create primary IP' }).first().click()
  const create = page.getByRole('dialog')
  await create.locator('input[name=name]').fill(NAME)
  await create.locator('select[name=location]').selectOption({ label: 'home' })
  await create.locator('select[name=type]').selectOption('ipv4')
  await create.locator('input[name=keep]').check()
  await create.getByRole('button', { name: 'Create primary IP' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/primary-ips\/\d+$/)
  const ipPath = new URL(page.url()).pathname
  await expect(page.getByRole('heading', { level: 1, name: NAME })).toBeVisible()
  const ip = (await page.locator('dt:text-is("Address") + dd').textContent())!.trim()
  expect(ip).toMatch(/^203\.0\.113\.\d+$/)
  await expectNoLeaks(page)

  // Server A takes it.
  const a = await createWithIP(page)
  await expect(page.getByRole('definition').filter({ hasText: ip }).first()).toBeVisible()
  const row = page.locator('[data-primary-ip=ipv4]')
  await expect(row).toContainText(NAME)
  await expectNoLeaks(page)

  // Deleting A keeps the address: its box starts ticked.
  await page.getByRole('button', { name: 'Delete server' }).click()
  const del = page.getByRole('dialog')
  await expect(del.getByLabel(`Keep ${ip} in this project`)).toBeChecked()
  await expect(del).toContainText('Kept IPs stay unassigned and count toward quota.')
  await del.locator('input[name=confirm_name]').fill(a)
  await del.getByRole('button', { name: 'Delete server' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/, { timeout: 60_000 })
  await expect(page.getByText(`${a} deleted`, { exact: true })).toBeVisible()

  await page.goto(ipPath)
  await expect(page.getByRole('definition').filter({ hasText: 'Not assigned' })).toBeVisible({
    timeout: 60_000,
  })
  await expectNoLeaks(page)

  // Server B takes it again.
  const b = await createWithIP(page)
  const bPath = new URL(page.url()).pathname
  await expect(page.locator('[data-primary-ip=ipv4]')).toContainText(NAME)

  // A running server can't swap it.
  await expect(page.getByText('Stop the server first.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Unassign IPv4' })).toBeDisabled()
  await page.getByRole('button', { name: 'Shut down' }).click()
  await expect(serverHeader(page).getByText('Off', { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expect(page.getByRole('button', { name: 'Unassign IPv4' })).toBeEnabled()
  await page.getByRole('button', { name: 'Unassign IPv4' }).click()
  const confirm = page.getByRole('dialog')
  await expect(confirm).toContainText('This server will have no public IPv4.')
  await expect(confirm).toContainText('The new address takes effect when the server starts.')
  await confirm.getByRole('button', { name: 'Unassign', exact: true }).click()
  await expect(page.locator('[data-primary-ip=ipv4]')).toContainText('None', { timeout: 60_000 })
  await expectNoLeaks(page)

  // Unassigned, it can be deleted.
  await page.goto(ipPath)
  await expect(page.getByRole('definition').filter({ hasText: 'Not assigned' })).toBeVisible({
    timeout: 60_000,
  })
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toBeEnabled({
    timeout: 60_000,
  })
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const gone = page.getByRole('dialog')
  await gone.locator('input[name=confirm_name]').fill(NAME)
  await gone.getByRole('button', { name: 'Delete primary IP' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/primary-ips$/, { timeout: 60_000 })
  await expect(page.getByText(`${NAME} deleted`, { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: new RegExp(NAME) })).toHaveCount(0)
  await expectNoLeaks(page)

  // Tidy up server B.
  await page.goto(bPath)
  await page.getByRole('button', { name: 'Delete server' }).click()
  const last = page.getByRole('dialog')
  await last.locator('input[name=confirm_name]').fill(b)
  await last.getByRole('button', { name: 'Delete server' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/, { timeout: 60_000 })
})
