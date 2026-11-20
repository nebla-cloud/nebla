import { expect, type Page } from '@playwright/test'

// Infrastructure words that must never reach a project page.
export const LEAKS = /vmid|local-lvm|vmbr|pve1|pve2|ceph|imp\b|proxmox/i

export async function expectNoLeaks(page: Page) {
  expect(await page.content()).not.toMatch(LEAKS)
}

export async function login(page: Page) {
  await page.goto('/login')
  await page.locator('input[name=email]').fill('admin@example.test')
  await page.locator('input[name=password]').fill('correct-horse-battery')
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

export function serverHeader(page: Page) {
  return page.locator('header').filter({ has: page.getByRole('heading', { level: 1 }) })
}

// Creates a server with the form's defaults and waits for it to run; returns its name.
export async function createServer(page: Page): Promise<string> {
  await page.getByRole('navigation').getByRole('link', { name: 'Servers' }).click()
  await page.getByRole('link', { name: 'Create server' }).first().click()
  await expect(page.getByRole('heading', { name: 'Create server' })).toBeVisible()
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
