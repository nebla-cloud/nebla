import { expect, test } from '@playwright/test'
import { createServer, expectNoLeaks, serverHeader } from './helpers'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as {
  pve: { url: string; token_id: string; token_secret: string; ca_fingerprint: string }
}
const SSH_KEY =
  'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKhCY6tff8uCIx8bJ18sUvMxanQaGQMcbCJCwbZXX8o6 e2e@nebla'

test('first run to first server', async ({ page }) => {
  const step = (title: string) => page.locator('[data-step]').filter({ hasText: title })
  const nav = page.getByRole('navigation')
  const snapshot = () => expectNoLeaks(page)

  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await page.locator('input[name=name]').fill('Admin')
  await page.locator('input[name=email]').fill('admin@example.test')
  await page.locator('input[name=password]').fill('correct-horse-battery')
  await page.getByRole('button', { name: 'Create account' }).click()

  await expect(page.getByRole('heading', { name: 'Get Nebla ready' })).toBeVisible()

  await step('Connect Proxmox').getByRole('link', { name: 'Connect' }).click()
  await page.getByRole('button', { name: 'Connect a location' }).click()
  await page.getByRole('button', { name: 'Connect manually' }).click()
  await page.locator('input[name=name]').fill('home')
  await page.locator('input[name=url]').fill(harness.pve.url)
  await page.locator('input[name=token_id]').fill(harness.pve.token_id)
  await page.locator('input[name=token_secret]').fill(harness.pve.token_secret)
  await page.locator('input[name=ca_fingerprint]').fill(harness.pve.ca_fingerprint)
  await page.locator('input[name=disk_storage]').fill('ceph')
  await page.locator('input[name=iso_storage]').fill('local')
  await page.locator('input[name=import_storage]').fill('imp')
  await page.getByRole('button', { name: 'Connect location' }).click()
  await expect(page).toHaveURL(/\/admin\/locations\/\d+$/)
  await nav.getByRole('link', { name: 'Get ready' }).click()
  await expect(step('Connect Proxmox')).toHaveAttribute('data-state', 'done')

  await step('Turn on OS images').getByRole('link', { name: 'Choose images' }).click()
  await page.getByRole('button', { name: 'Enable Debian 13 in home' }).click()
  await expect(
    page.getByRole('button', { name: 'Debian 13 in home: Ready. Show details' }),
  ).toBeVisible({
    timeout: 60_000,
  })

  await nav.getByRole('link', { name: 'Get ready' }).click()
  await expect(step('Turn on OS images')).toHaveAttribute('data-state', 'done')
  await step('Add server types').getByRole('link', { name: 'Add' }).click()
  await page.getByRole('button', { name: 'New server type' }).first().click()
  const typeForm = page.getByRole('dialog')
  await typeForm.locator('input[name=name]').fill('small-2')
  await typeForm.locator('input[name=cores]').fill('2')
  await typeForm.locator('input[name=memory_gb]').fill('4')
  await typeForm.locator('input[name=disk_gb]').fill('40')
  await typeForm.getByRole('button', { name: 'Create server type' }).click()
  await expect(page.getByText('2 vCPU, 4 GB, 40 GB')).toBeVisible()

  await nav.getByRole('link', { name: 'Get ready' }).click()
  await expect(step('Add server types')).toHaveAttribute('data-state', 'done')
  await step('Add an IP pool').getByRole('link', { name: 'Add' }).click()
  await page.getByRole('button', { name: 'New IP pool' }).first().click()
  const poolForm = page.getByRole('dialog')
  await poolForm.locator('select[name=location_id]').selectOption({ label: 'home' })
  await poolForm.locator('select[name=family]').selectOption('ipv4')
  await poolForm.locator('input[name=bridge]').fill('vmbr0')
  await poolForm.locator('input[name=cidr]').fill('203.0.113.0/24')
  await poolForm.locator('input[name=gateway]').fill('203.0.113.1')
  await poolForm.locator('input[name=range_start]').fill('203.0.113.10')
  await poolForm.locator('input[name=range_end]').fill('203.0.113.20')
  await poolForm.getByRole('button', { name: 'Create IP pool' }).click()
  await expect(page.getByText('0 of 11 addresses in use')).toBeVisible()
  await expect(nav.getByRole('link', { name: 'Get ready' })).toHaveCount(0)

  await page.getByRole('link', { name: 'Go to projects' }).click()
  await expect(page).toHaveURL(/\/projects$/)
  await snapshot()
  await page.locator('input[name=name]').fill('Home lab')
  await page.getByRole('button', { name: 'Create project' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  await expect(page.getByText('No servers yet')).toBeVisible()
  await snapshot()

  await nav.getByRole('link', { name: 'SSH keys' }).click()
  await page.getByRole('button', { name: 'Add key' }).first().click()
  const keyForm = page.getByRole('dialog')
  await keyForm.locator('textarea[name=public_key]').fill(SSH_KEY)
  await keyForm.getByRole('button', { name: 'Add key' }).click()
  await expect(page.getByText('e2e@nebla', { exact: true })).toBeVisible()
  await snapshot()

  const serverName = await createServer(page)
  const header = serverHeader(page)
  await snapshot()

  await page.getByRole('button', { name: 'Reboot' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Rebooting' })).toBeVisible()
  await expect(page.getByText(`${serverName} rebooted`, { exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expect(header.getByText('Running', { exact: true })).toBeVisible({ timeout: 60_000 })
  await snapshot()

  await page.getByRole('button', { name: 'Delete server' }).click()
  const confirm = page.getByRole('dialog')
  await confirm.locator('input[name=confirm_name]').fill(serverName)
  await confirm.getByRole('button', { name: 'Delete server' }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/, { timeout: 60_000 })
  await expect(page.getByText('No servers yet')).toBeVisible({ timeout: 60_000 })
  await snapshot()
})
