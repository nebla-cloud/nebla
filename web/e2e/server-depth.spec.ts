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
import { adminPage, createServer, expectNoLeaks, serverHeader } from './helpers'

test.describe.configure({ mode: 'serial' })

let page: Page
let serverName = ''
let serverPath = ''

const tab = (name: string) =>
  page.getByRole('navigation', { name: 'Server sections' }).getByRole('link', { name })
const status = (label: string) => serverHeader(page).getByText(label, { exact: true })

async function openServer() {
  await page.goto(serverPath)
  await expect(page.getByRole('heading', { level: 1, name: serverName })).toBeVisible()
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(240_000)
  page = await adminPage(browser)

  await page.goto('/admin/os-images')
  await page.getByRole('button', { name: 'Enable Ubuntu 24.04 in home' }).click()
  await expect(
    page.getByRole('button', { name: 'Ubuntu 24.04 in home: Ready. Show details' }),
  ).toBeVisible({ timeout: 60_000 })

  await page.goto('/admin/server-types')
  await page.getByRole('button', { name: 'New server type' }).first().click()
  const typeForm = page.getByRole('dialog')
  await typeForm.locator('input[name=name]').fill('medium-4')
  await typeForm.locator('input[name=cores]').fill('4')
  await typeForm.locator('input[name=memory_gb]').fill('8')
  await typeForm.locator('input[name=disk_gb]').fill('80')
  await typeForm.getByRole('button', { name: 'Create server type' }).click()
  await expect(page.getByText('4 vCPU, 8 GB, 80 GB')).toBeVisible()

  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  serverName = await createServer(page)
  serverPath = new URL(page.url()).pathname
})

test.afterAll(async () => {
  await page?.context().close()
})

test('labels and filter', async () => {
  await openServer()
  await page.getByRole('button', { name: 'Edit labels' }).click()
  await page.getByRole('button', { name: 'Add label' }).click()
  await page.getByRole('textbox', { name: 'Label key 1' }).fill('env')
  await page.getByRole('textbox', { name: 'Label value 1' }).fill('prod')
  await page.getByRole('button', { name: 'Save labels' }).click()
  await expect(page.getByRole('button', { name: 'Edit labels' })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Labels' }).first()).toContainText('env=prod')
  await expectNoLeaks(page)

  await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link').click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  const filter = page.locator('input[name=labels]')
  const row = page.getByRole('link', { name: new RegExp(serverName) })

  await filter.fill('env=prod')
  await expect(page).toHaveURL(/labels=env%3Dprod|labels=env=prod/)
  await expect(row).toBeVisible()

  await filter.fill('env=dev')
  await expect(page.getByText('No servers match this filter.')).toBeVisible()
  await expect(row).toHaveCount(0)
  await expectNoLeaks(page)
})

test('graphs', async () => {
  await openServer()
  await expect(page.locator('[data-metric="memory"]')).toBeVisible()
  await tab('Graphs').click()
  const charts = page.getByRole('img', { name: /over time$/ })
  await expect(charts.first().or(page.getByText('No data for this range'))).toBeVisible()
  if (await charts.count()) {
    for (const name of ['CPU over time', 'Memory over time', 'Disk over time', 'Network over time'])
      await expect(page.getByRole('img', { name })).toBeVisible()
  }
  await expectNoLeaks(page)
})

test('console smoke', async () => {
  await openServer()
  const grant = page.waitForRequest(
    (r) => r.method() === 'POST' && /\/servers\/\d+\/actions\/request_console$/.test(r.url()),
  )
  await tab('Console').click()
  await expect(page.getByRole('status').filter({ hasText: 'Connecting' })).toBeVisible()
  await grant
  await expectNoLeaks(page)
})

test('rebuild', async () => {
  await openServer()
  await tab('Rebuild').click()
  const ubuntu = page.getByRole('radio', { name: 'ubuntu-24.04' })
  const debian = page.getByRole('radio', { name: 'debian-13' })
  await expect(page.getByRole('radio', { checked: true })).toHaveCount(1)
  const toUbuntu = await debian.isChecked()
  const other = toUbuntu ? ubuntu : debian
  const otherName = toUbuntu ? 'ubuntu-24.04' : 'debian-13'
  await other.check()
  await expectNoLeaks(page)
  await page.getByRole('button', { name: 'Rebuild', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(serverName)
  await confirm.getByRole('button', { name: 'Rebuild' }).click()

  await expect(page).toHaveURL(new RegExp(`${serverPath}$`))
  await expect(page.getByText(`${serverName} rebuilt`, { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expect(status('Running')).toBeVisible({ timeout: 60_000 })
  await expect(
    page.getByRole('definition').filter({ hasText: new RegExp(`^\\s*${otherName}\\s*$`) }),
  ).toBeVisible()
  await expectNoLeaks(page)
})

test('resize', async () => {
  await openServer()
  await tab('Resize').click()
  const note = page.getByText('Shut the server down to resize it.')
  await expect(note).toBeVisible()
  await note.locator('..').getByRole('button', { name: 'Shut down' }).click()
  await expect(status('Off')).toBeVisible({ timeout: 60_000 })

  await page.getByRole('radio', { name: /medium-4/ }).check()
  await expectNoLeaks(page)
  await page.getByRole('button', { name: 'Resize', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`${serverPath}$`))
  await expect(page.getByText(`${serverName} resized`, { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expect(status('Off')).toBeVisible({ timeout: 60_000 })
  await expect(page.getByRole('definition').filter({ hasText: /^medium-4: / })).toBeVisible()
  await expectNoLeaks(page)
})

test('activity', async () => {
  await page.goto(serverPath.replace(/servers\/\d+$/, 'activity'))
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
  for (const title of ['Rebuild', 'Resize']) {
    const row = page.getByRole('listitem').filter({
      has: page.getByText(title, { exact: true }),
    })
    await expect(row.first()).toContainText('Done')
  }
  await expectNoLeaks(page)
})
