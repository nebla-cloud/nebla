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

import { expect, test, type Browser, type Page, type Response } from '@playwright/test'
import { addVirtualAuthenticator, adminPage, login, totpCode } from './helpers'

const PASSWORD = 'correct-horse-battery'

const USERS = ['pk-login', 'pk-totp', 'pk-rename', 'pk-autofill'].map((n) => `${n}@example.test`)

// Each spec gets its own user so the per-account login limiter never starves them.
test.beforeAll(async ({ browser }) => {
  const admin = await adminPage(browser)
  try {
    for (const email of USERS) {
      const made = await admin.request.post('/v1/admin/users', {
        data: { email, password: PASSWORD, name: email.split('@')[0] },
      })
      expect([201, 409]).toContain(made.status())
    }
  } finally {
    await admin.context().close()
  }
})

// Counts passkey begin requests per page, including the one login autofill sends on mount.
const beginCount = new WeakMap<Page, number>()

async function userPage(browser: Browser, email: string, autofill = false): Promise<Page> {
  const page = await (await browser.newContext()).newPage()
  await addVirtualAuthenticator(page)
  // Unless testing autofill, stop the login page's autofill ceremony from logging in before the button.
  if (!autofill)
    await page.addInitScript(
      'PublicKeyCredential.isConditionalMediationAvailable = () => Promise.resolve(false)',
    )
  page.on('request', (r) => {
    if (r.method() === 'POST' && r.url().endsWith('/v1/auth/passkey/begin'))
      beginCount.set(page, (beginCount.get(page) ?? 0) + 1)
  })
  await login(page, email)
  return page
}

async function logOut(page: Page) {
  await page.getByRole('button', { name: /@example\.test/ }).click()
  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL(/\/login/)
}

async function addPasskey(page: Page, code?: string) {
  await page.goto('/account')
  await page.getByRole('button', { name: 'Add a passkey' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Add a passkey', { exact: true }).first()).toBeVisible()
  await dialog.locator('input[name=password]').fill(PASSWORD)
  if (code) await dialog.locator('input[name=code]').fill(code)
  await dialog.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByText('Passkey added', { exact: true })).toBeVisible()
}

async function passkeyLogin(page: Page) {
  const button = page.getByRole('button', { name: 'Log in with a passkey' })
  await expect(button).toBeEnabled()
  // Let the mount-time autofill begin finish so the next begin is the button's.
  await page.waitForLoadState('networkidle')
  const before = beginCount.get(page) ?? 0
  const begun = page.waitForResponse(
    (r) => r.request().method() === 'POST' && r.url().endsWith('/v1/auth/passkey/begin'),
  )
  const finished = page.waitForResponse(
    (r) => r.request().method() === 'POST' && r.url().endsWith('/v1/auth/passkey/finish'),
  )
  await button.click()
  expect((await begun).status()).toBe(200)
  expect(beginCount.get(page)).toBe(before + 1)
  expect((await finished).status()).toBe(200)
  await expect(page).not.toHaveURL(/\/login/)
}

async function removeAll(page: Page) {
  await page.goto('/account')
  await page
    .getByRole('button', { name: /^Actions for / })
    .first()
    .click()
  await page.getByRole('menuitem', { name: 'Remove' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Remove passkey' }).click()
  await expect(page.getByText('Passkey removed', { exact: true })).toBeVisible()
}

test('passkey login', async ({ browser }) => {
  const page = await userPage(browser, USERS[0]!)
  try {
    await addPasskey(page)
    await expect(page.getByRole('button', { name: /^Actions for / })).toHaveCount(1)
    await expect(page.getByText(/^Added /)).toBeVisible()
    await expect(page.getByText('Passkey', { exact: true })).toBeVisible()

    await logOut(page)
    await passkeyLogin(page)
    await expect(page).toHaveURL(/\/projects|\/p\//)

    await removeAll(page)
  } finally {
    await page.context().close()
  }
})

test('passkey skips totp', async ({ browser }) => {
  const page = await userPage(browser, USERS[1]!)
  try {
    await page.goto('/account')
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Turn on' }).click()
    const key = await page.getByLabel('Key', { exact: true }).inputValue()
    const secret = key.replace(/\s/g, '')
    await page.locator('input[name=code]').fill(totpCode(secret))
    await page.getByRole('button', { name: 'Verify and turn on' }).click()
    await expect(page.getByText('Two-factor login is on.')).toBeVisible()

    await addPasskey(page, totpCode(secret))
    await logOut(page)
    await passkeyLogin(page)
    expect((await page.request.get('/v1/me')).status()).toBe(200)

    await page.goto('/account')
    await expect(page.getByText('Two-factor login is on.')).toBeVisible()
    await page.locator('input[name=code]').first().fill(totpCode(secret))
    await page.locator('input[name=password]').first().fill(PASSWORD)
    await page.getByRole('button', { name: 'Turn off' }).click()
    await expect(page.getByRole('button', { name: 'Turn on' })).toBeVisible()

    await removeAll(page)
  } finally {
    await page.context().close()
  }
})

test('rename and remove', async ({ browser }) => {
  const page = await userPage(browser, USERS[2]!)
  const other = await (await browser.newContext()).newPage()
  try {
    await addPasskey(page)
    await page.getByRole('button', { name: /^Actions for / }).click()
    await page.getByRole('menuitem', { name: 'Rename' }).click()
    const rename = page.getByRole('dialog')
    await rename.locator('input[name=name]').fill('Work laptop')
    await rename.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Work laptop', { exact: true })).toBeVisible()

    await login(other, USERS[2]!)
    expect((await other.request.get('/v1/me')).status()).toBe(200)

    await page.getByRole('button', { name: 'Actions for Work laptop' }).click()
    await page.getByRole('menuitem', { name: 'Remove' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Remove passkey' }).click()
    await expect(page.getByText('Passkey removed', { exact: true })).toBeVisible()

    expect((await other.request.get('/v1/me')).status()).toBe(401)
  } finally {
    await other.context().close()
    await page.context().close()
  }
})

test('passkey autofill login', async ({ browser }) => {
  const page = await userPage(browser, USERS[3]!, true)
  try {
    await addPasskey(page)
    const isPost = (r: Response, path: string) =>
      r.request().method() === 'POST' && r.url().endsWith(path)
    const loggedOut = page.waitForResponse((r) => isPost(r, '/v1/auth/logout'))
    // Autofill leaves /login within ~50ms, faster than toHaveURL polls, so wait on the navigation.
    const atLogin = page.waitForURL(/\/login/)
    const begun = page.waitForResponse((r) => isPost(r, '/v1/auth/passkey/begin'))
    const finished = page.waitForResponse((r) => isPost(r, '/v1/auth/passkey/finish'))
    await page.getByRole('button', { name: /@example\.test/ }).click()
    await page.getByRole('button', { name: 'Log out' }).click()
    expect((await loggedOut).status()).toBe(204)
    await atLogin
    // No click: the login page's autofill ceremony signs in on its own.
    expect((await begun).status()).toBe(200)
    expect((await finished).status()).toBe(200)
    await expect(page).not.toHaveURL(/\/login/)
    expect((await page.request.get('/v1/me')).status()).toBe(200)
    await removeAll(page)
  } finally {
    await page.context().close()
  }
})
