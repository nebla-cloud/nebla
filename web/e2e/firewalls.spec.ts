import { expect, test, type Page } from '@playwright/test'
import { adminPage, createServer, expectNoLeaks } from './helpers'

test.describe.configure({ mode: 'serial' })

let page: Page
let serverPath = ''
let serverName = ''

test.beforeAll(async ({ browser }) => {
  test.setTimeout(240_000)
  page = await adminPage(browser)
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  serverName = await createServer(page)
  serverPath = new URL(page.url()).pathname
})

test.afterAll(async () => {
  await page?.context().close()
})

test('label the server', async () => {
  await page.getByRole('button', { name: 'Edit labels' }).click()
  await page.getByRole('button', { name: 'Add label' }).click()
  await page.getByRole('textbox', { name: 'Label key 1' }).fill('role')
  await page.getByRole('textbox', { name: 'Label value 1' }).fill('web')
  await page.getByRole('button', { name: 'Save labels' }).click()
  await expect(page.getByRole('list', { name: 'Labels' }).first()).toContainText('role=web')
})

test('create a firewall with a rule', async () => {
  await page
    .getByRole('complementary', { name: 'Project menu' })
    .getByRole('link', { name: 'Firewalls' })
    .click()
  await page.getByRole('button', { name: 'Create firewall' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('input[name=name]').fill('web')
  await dialog.getByRole('button', { name: 'Create firewall' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'web' })).toBeVisible()

  await page.getByRole('button', { name: 'Add inbound rule' }).click()
  await page.getByLabel('Port, Inbound rule 1').fill('22')
  await page.getByLabel('Sources, Inbound rule 1').fill('0.0.0.0/0')
  await page.getByRole('button', { name: 'Save rules' }).click()
  await expect(page.getByText('Rules saved')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save rules' })).toBeDisabled()
  await expectNoLeaks(page)
})

test('apply to a label selector', async () => {
  await page.getByRole('button', { name: 'Add', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('radio', { name: /^Servers by label/ }).check()
  await dialog.locator('input[name=selector]').fill('role=web')
  await expect(dialog.getByText('Matches 1 server right now')).toBeVisible()
  await expect(dialog.getByRole('list', { name: 'Matching servers' })).toContainText(serverName)
  await dialog.getByRole('button', { name: 'Apply', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText('Matches 1 server', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Applying firewalls, 100%')).toBeVisible({ timeout: 60_000 })
  await expectNoLeaks(page)
})

test('the server shows the firewall', async () => {
  await page.goto(serverPath)
  await page
    .getByRole('navigation', { name: 'Server sections' })
    .getByRole('link', { name: 'Firewalls' })
    .click()
  const row = page.getByRole('list', { name: 'Firewalls' }).getByRole('link', { name: /web/ })
  await expect(row).toContainText('via role=web')
  await expectNoLeaks(page)
})

test('delete the firewall', async () => {
  await page.getByRole('list', { name: 'Firewalls' }).getByRole('link', { name: /web/ }).click()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox').fill('web')
  await dialog.getByRole('button', { name: 'Delete firewall' }).click()
  await expect(page.getByText('No firewalls yet')).toBeVisible()
})
