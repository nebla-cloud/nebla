import { createHmac } from 'node:crypto'
import { expect, type Browser, type CDPSession, type Page } from '@playwright/test'

// Infrastructure words that must never reach a project page.
export const LEAKS = /vmid|local-lvm|vmbr|pve1|pve2|ceph|\bimp\b|proxmox/i

export async function expectNoLeaks(page: Page) {
  expect(await page.content()).not.toMatch(LEAKS)
}

export async function login(page: Page, email = 'admin@example.test') {
  await page.goto('/login')
  await page.locator('input[name=email]').fill(email)
  await page.locator('input[name=password]').fill('correct-horse-battery')
  await page.getByRole('button', { name: 'Log in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

// The admin session the admin-session project saves, so the suite logs the admin in once.
export function adminState(): string {
  const path = process.env.NEBLA_E2E_ADMIN_STATE
  if (!path) throw new Error('NEBLA_E2E_ADMIN_STATE is not set; run through global setup')
  return path
}

export async function adminPage(browser: Browser): Promise<Page> {
  return (await browser.newContext({ storageState: adminState() })).newPage()
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

const B32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

function base32Decode(s: string): Buffer {
  let bits = ''
  for (const ch of s.replace(/[\s=]/g, '').toUpperCase()) {
    const v = B32.indexOf(ch)
    if (v < 0) throw new Error(`bad base32 character ${ch}`)
    bits += v.toString(2).padStart(5, '0')
  }
  const bytes: number[] = []
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(bytes)
}

// RFC 6238 TOTP: SHA-1, 30 s step, 6 digits. `at` is in milliseconds.
export function totpCode(secret: string, at: number = Date.now()): string {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)))
  const mac = createHmac('sha1', base32Decode(secret)).update(counter).digest()
  const off = mac[mac.length - 1]! & 0xf
  const n = (mac.readUInt32BE(off) & 0x7fffffff) % 1_000_000
  return String(n).padStart(6, '0')
}

// Adds a CTAP2 platform authenticator that approves every ceremony.
export async function addVirtualAuthenticator(
  page: Page,
): Promise<{ cdp: CDPSession; id: string }> {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })
  return { cdp, id: authenticatorId }
}
