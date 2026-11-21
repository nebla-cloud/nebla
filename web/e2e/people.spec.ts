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
import { adminPage, expectNoLeaks, totpCode } from './helpers'

const PASSWORD = 'correct-horse-battery'

async function homeLab(page: Page): Promise<number> {
  await page.goto('/projects')
  await page.getByRole('link', { name: /Home lab/ }).click()
  await expect(page).toHaveURL(/\/p\/\d+\/servers$/)
  return Number(/\/p\/(\d+)\//.exec(page.url())![1])
}

async function logOut(page: Page) {
  await page.getByRole('button', { name: /@example\.test/ }).click()
  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL(/\/login/)
}

test('tokens: create, use and revoke', async ({ browser, request: api }) => {
  const page = await adminPage(browser)
  try {
    const projectId = await homeLab(page)
    const nav = page.getByRole('navigation')
    await nav.getByRole('link', { name: 'API tokens' }).click()
    await expectNoLeaks(page)
    await page.getByRole('button', { name: 'Create token' }).first().click()
    const dialog = page.getByRole('dialog')
    await dialog.locator('input[name=name]').fill('e2e token')
    await dialog.getByText('Read and write', { exact: true }).click()
    await dialog.getByRole('button', { name: 'Create token' }).click()
    await expect(dialog.getByText("You won't see it again.")).toBeVisible()
    const token = await dialog.getByLabel('Your token').inputValue()
    expect(token).not.toBe('')
    await dialog.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Close anyway' }).click()
    await expect(page.getByText('e2e token', { exact: true })).toBeVisible()
    await expect(page.getByText('Read and write', { exact: true })).toBeVisible()
    await expectNoLeaks(page)

    const call = () =>
      api.get('/v1/servers', {
        headers: { Authorization: `Bearer ${token}`, 'Project-ID': String(projectId) },
      })
    expect((await call()).status()).toBe(200)

    await page.getByRole('button', { name: 'Revoke e2e token' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Revoke token' }).click()
    await expect(page.getByText('Revoked e2e token', { exact: true })).toBeVisible()
    expect((await call()).status()).toBe(401)
  } finally {
    await page.context().close()
  }
})

test('invite: join as member, become read-only, leave', async ({ browser }) => {
  const admin = await adminPage(browser)
  const friendCtx = await browser.newContext()
  try {
    const projectId = await homeLab(admin)
    await admin.getByRole('navigation').getByRole('link', { name: 'Members' }).click()
    await admin.getByRole('button', { name: 'Invite' }).click()
    const invite = admin.getByRole('dialog')
    await invite.locator('input[name=email]').fill('friend@example.test')
    await invite.locator('select[name=role]').selectOption({ label: 'Member' })
    await invite.getByRole('button', { name: 'Create invite' }).click()
    const link = await invite.getByLabel('Invite link').inputValue()
    expect(link).toContain('/invite/')
    await invite.getByRole('button', { name: 'Done' }).click()
    await expect(admin.getByText('friend@example.test')).toBeVisible()

    const friend = await friendCtx.newPage()
    await friend.goto(link)
    await expect(friend.getByRole('heading', { name: 'Join Home lab' })).toBeVisible()
    await friend.locator('input[name=name]').fill('Friend')
    await friend.locator('input[name=password]').fill(PASSWORD)
    await friend.getByRole('button', { name: 'Create account and join' }).click()
    await expect(friend).toHaveURL(new RegExp(`/p/${projectId}/servers$`))
    const friendNav = friend.getByRole('navigation')
    await friendNav.getByRole('link', { name: 'Members' }).click()
    const ownRow = friend.getByRole('listitem').filter({ hasText: '(you)' })
    await expect(ownRow).toContainText('Member')
    await expect(ownRow).not.toContainText('Read-only')
    await expectNoLeaks(friend)

    await admin.reload()
    await admin.getByLabel('Role for Friend').selectOption({ label: 'Read-only' })
    await admin.getByRole('dialog').getByRole('button', { name: 'Change role' }).click()
    await expect(admin.getByLabel('Role for Friend')).toHaveValue('read_only')

    await friend.reload()
    await expect(friend.getByRole('listitem').filter({ hasText: '(you)' })).toContainText(
      'Read-only',
    )
    await expect(
      friend.getByRole('navigation').getByRole('link', { name: 'API tokens' }),
    ).toHaveCount(0)
    await expectNoLeaks(friend)

    await friend.getByRole('navigation').getByRole('link', { name: 'Settings' }).click()
    await friend.getByRole('button', { name: 'Leave project' }).click()
    await friend.getByRole('dialog').getByRole('button', { name: 'Leave project' }).click()
    await expect(friend).toHaveURL(/\/projects$/)
    await expect(friend.getByText('Create your first project')).toBeVisible()
    await friend.goto(`/p/${projectId}/servers`)
    await expect(friend.getByRole('heading', { name: "We can't find that page" })).toBeVisible()
  } finally {
    await friendCtx.close()
    await admin.context().close()
  }
})

test('totp: turn on, log in with a code, turn off', async ({ browser }) => {
  const admin = await adminPage(browser)
  const email = 'totp-user@example.test'
  const ctx = await browser.newContext()
  try {
    const made = await admin.request.post('/v1/admin/users', {
      data: { email, password: PASSWORD, name: 'Totp User' },
    })
    expect(made.status()).toBe(201)

    const page = await ctx.newPage()
    await page.goto('/login')
    await page.locator('input[name=email]').fill(email)
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Log in', exact: true }).click()
    await expect(page).not.toHaveURL(/\/login/)

    await page.goto('/account')
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Turn on' }).click()
    const key = await page.getByLabel('Key', { exact: true }).inputValue()
    const secret = key.replace(/\s/g, '')
    await page.locator('input[name=code]').fill(totpCode(secret))
    await page.getByRole('button', { name: 'Verify and turn on' }).click()
    await expect(page.getByText('Two-factor login is on.')).toBeVisible()

    await logOut(page)
    await page.locator('input[name=email]').fill(email)
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Log in', exact: true }).click()
    const code = page.locator('input[name=totp_code]')
    await expect(code).toBeVisible()
    await code.fill(totpCode(secret))
    await page.getByRole('button', { name: 'Log in', exact: true }).click()
    await expect(page).not.toHaveURL(/\/login/)

    await page.goto('/account')
    await expect(page.getByText('Two-factor login is on.')).toBeVisible()
    await page.locator('input[name=code]').fill(totpCode(secret))
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Turn off' }).click()
    await expect(page.getByRole('button', { name: 'Turn on' })).toBeVisible()
  } finally {
    await ctx.close()
    await admin.context().close()
  }
})

test('signup: self-signup on, create account, off again', async ({ browser }) => {
  const admin = await adminPage(browser)
  const ctx = await browser.newContext()
  try {
    const on = await admin.request.put('/v1/admin/settings', { data: { self_signup: true } })
    expect(on.ok()).toBeTruthy()

    const page = await ctx.newPage()
    await page.goto('/login')
    await page.getByRole('link', { name: 'Create an account' }).click()
    await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible()
    await page.locator('input[name=name]').fill('Signed Up')
    await page.locator('input[name=email]').fill('signup@example.test')
    await page.locator('input[name=password]').fill(PASSWORD)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page.getByText('Create your first project')).toBeVisible()
    await expectNoLeaks(page)
  } finally {
    const off = await admin.request.put('/v1/admin/settings', { data: { self_signup: false } })
    expect(off.ok()).toBeTruthy()
    await ctx.close()
    await admin.context().close()
  }
})
