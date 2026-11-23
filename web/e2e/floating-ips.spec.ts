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
import { adminState, expectNoLeaks } from './helpers'

// eslint-disable-next-line no-empty-pattern -- Playwright fixtures must destructure their first argument.
test.use({ storageState: async ({}, use) => use(adminState()) })

const PANEL =
  'New and rebuilt servers set this up automatically. For a running server, run these commands.'

test('create, assign, move and delete a floating IP', async ({ page }) => {
  test.setTimeout(240_000)
  const name = 'e2e-vip'
  const nav = page.getByRole('complementary', { name: 'Project menu' })

  // server-depth and firewalls each leave a running server in Home lab.
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)

  await nav.getByRole('link', { name: 'Floating IPs', exact: true }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/floating-ips$/)
  await expect(page.getByText('No floating IPs yet')).toBeVisible()
  await expectNoLeaks(page)

  await page.getByRole('button', { name: 'Create floating IP' }).click()
  const create = page.getByRole('dialog')
  await create.locator('input[name=name]').fill(name)
  await create.locator('select[name=location]').selectOption({ label: 'home' })
  await create.locator('select[name=type]').selectOption('ipv4')
  await create.getByRole('button', { name: 'Create floating IP' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/floating-ips\/\d+$/)
  const fipPath = new URL(page.url()).pathname
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible()
  await expect(page.getByRole('definition').filter({ hasText: 'Not assigned' })).toBeVisible()
  const ip = (await page.locator('dt:text-is("Address") + dd').textContent())!.trim()
  expect(ip).toMatch(/^203\.0\.113\.\d+$/)
  await expectNoLeaks(page)

  // Assign to the first server the picker offers.
  await page.getByRole('button', { name: 'Assign', exact: true }).click()
  const assign = page.getByRole('dialog', { name: `Assign ${name}` })
  const pick = assign.locator('select[name=server]')
  const first = (await pick.locator('option:not([disabled])').first().textContent())!.trim()
  await pick.selectOption({ label: first })
  await assign.getByRole('button', { name: 'Assign', exact: true }).click()
  await expect(assign.getByRole('status')).toContainText('Assigning floating IP')

  const guest = page.getByRole('dialog', { name: 'Run this on the server' })
  await expect(guest).toBeVisible({ timeout: 60_000 })
  await expect(guest).toContainText(PANEL)
  await expect(guest).toContainText(first)
  await expect(guest.getByTestId('guest-apply')).toContainText(`ip addr add ${ip}/32 dev eth0`)
  await expect(guest.getByTestId('guest-persist')).toBeVisible()
  await expect(guest).not.toContainText('to stop using the address')
  await expectNoLeaks(page)
  await guest.getByRole('button', { name: 'Done' }).click()
  await expect(guest).toBeHidden()

  // The server's Overview links back to the floating IP.
  await page.getByRole('link', { name: first, exact: true }).click()
  await expect(page.getByRole('heading', { level: 1, name: first })).toBeVisible()
  await page.getByRole('link', { name: ip, exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`${fipPath}$`))

  // Move to another server; the previous one gets removal commands.
  await page.getByRole('button', { name: 'Move', exact: true }).click()
  const move = page.getByRole('dialog', { name: `Move ${name}` })
  const choices = move.locator('select[name=server]')
  await expect(
    choices.locator('option', { hasText: `${first} (Assigned here now)` }),
  ).toBeDisabled()
  const second = (await choices.locator('option:not([disabled])').first().textContent())!.trim()
  expect(second).not.toBe(first)
  await choices.selectOption({ label: second })
  await move.getByRole('button', { name: 'Move', exact: true }).click()

  await expect(guest).toBeVisible({ timeout: 60_000 })
  await expect(guest).toContainText(second)
  await expect(guest).toContainText(`Run this on ${first} to stop using the address`)
  await expect(guest.getByTestId('guest-remove')).toContainText(`ip addr del ${ip}/32 dev eth0`)
  await expectNoLeaks(page)
  await guest.getByRole('button', { name: 'Done' }).click()
  await expect(page.getByRole('definition').filter({ hasText: second })).toBeVisible()

  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(name)
  await confirm.getByRole('button', { name: 'Delete floating IP' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/floating-ips$/, { timeout: 60_000 })
  await expect(page.getByText(`${name} deleted`, { exact: true })).toBeVisible()
  await expect(page.getByText('No floating IPs yet')).toBeVisible({ timeout: 60_000 })
  await expectNoLeaks(page)
})
