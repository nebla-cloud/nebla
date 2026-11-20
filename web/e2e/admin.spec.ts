import { expect, test, type Browser, type BrowserContext, type Page } from '@playwright/test'
import { expectNoLeaks, login, totpCode } from './helpers'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as { fail_next: string }
const PASSWORD = 'correct-horse-battery'

type State = Awaited<ReturnType<BrowserContext['storageState']>>
let adminState: State

// One admin login for the file: the login limiter allows ten per email before throttling.
test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext()
  await login(await ctx.newPage())
  adminState = await ctx.storageState()
  await ctx.close()
})

async function adminPage(browser: Browser): Promise<Page> {
  return (await browser.newContext({ storageState: adminState })).newPage()
}

async function logOut(page: Page) {
  await page.getByRole('button', { name: /@example\.test/ }).click()
  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL(/\/login/)
}

function userRow(page: Page, email: string) {
  return page.getByRole('row').filter({ hasText: email })
}

async function rowAction(page: Page, name: string, item: string) {
  await page.getByRole('button', { name: `Actions for ${name}` }).click()
  await page.getByRole('menuitem', { name: item }).click()
}

async function makeUser(admin: Page, email: string, name: string) {
  const made = await admin.request.post('/v1/admin/users', {
    data: { email, password: PASSWORD, name },
  })
  expect(made.status()).toBe(201)
}

test('users: create, two-factor off, email change signs out, delete', async ({ browser }) => {
  const admin = await adminPage(browser)
  const ctx = await browser.newContext()
  const email = 'dana@example.test'
  const renamed = 'dana.k@example.test'
  try {
    await admin.goto('/admin/users')
    await admin.getByRole('button', { name: 'New user' }).click()
    const form = admin.getByRole('dialog')
    await form.locator('input[name=name]').fill('Dana')
    await form.locator('input[name=email]').fill(email)
    await form.locator('input[name=password]').fill(PASSWORD)
    await form.getByRole('button', { name: 'Create user' }).click()
    await expect(form).toBeHidden()
    await expect(userRow(admin, email)).toContainText('Off')

    const user = await ctx.newPage()
    await login(user, email)
    await user.goto('/account')
    await user.locator('input[name=password]').fill(PASSWORD)
    await user.getByRole('button', { name: 'Turn on' }).click()
    const secret = (await user.getByLabel('Key', { exact: true }).inputValue()).replace(/\s/g, '')
    await user.locator('input[name=code]').fill(totpCode(secret))
    await user.getByRole('button', { name: 'Verify and turn on' }).click()
    await expect(user.getByText('Two-factor login is on.')).toBeVisible()

    await admin.reload()
    await expect(userRow(admin, email)).toContainText('On')
    await rowAction(admin, 'Dana', 'Turn off two-factor')
    await admin.getByRole('dialog').getByRole('button', { name: 'Turn off two-factor' }).click()
    await expect(admin.getByText('Two-factor is off for Dana', { exact: true })).toBeVisible()
    await expect(userRow(admin, email)).toContainText('Off')

    await logOut(user)
    await login(user, email)
    await expect(user.locator('input[name=totp_code]')).toHaveCount(0)

    await rowAction(admin, 'Dana', 'Edit')
    const edit = admin.getByRole('dialog')
    await edit.locator('input[name=email]').fill(renamed)
    await expect(
      edit.getByText("They'll be signed out and log in with the new email."),
    ).toBeVisible()
    await edit.getByRole('button', { name: 'Save changes' }).click()
    await expect(edit).toBeHidden()
    await expect(userRow(admin, renamed)).toBeVisible()

    await user.goto('/account')
    await expect(user).toHaveURL(/\/login/)
    await login(user, renamed)

    await rowAction(admin, 'Dana', 'Delete')
    const del = admin.getByRole('dialog')
    await del.locator('input[name=confirm_name]').fill(renamed)
    await del.getByRole('button', { name: 'Delete user' }).click()
    await expect(admin.getByText(`Deleted ${renamed}`, { exact: true })).toBeVisible()
    await expect(userRow(admin, renamed)).toHaveCount(0)
  } finally {
    await ctx.close()
    await admin.context().close()
  }
})

test('users: deleting a sole owner hands the project to the admin', async ({ browser }) => {
  const admin = await adminPage(browser)
  const ctx = await browser.newContext()
  const email = 'owner@example.test'
  try {
    await makeUser(admin, email, 'Olive')
    const user = await ctx.newPage()
    await login(user, email)
    await user.goto('/projects')
    await user.locator('input[name=name]').fill('Orphan lab')
    await user.getByRole('button', { name: 'Create project' }).click()
    await expect(user).toHaveURL(/\/p\/\d+\/servers$/)

    await admin.goto('/admin/users')
    await rowAction(admin, 'Olive', 'Delete')
    const del = admin.getByRole('dialog')
    await del.locator('input[name=confirm_name]').fill(email)
    await del.getByRole('button', { name: 'Delete user' }).click()
    await expect(del.getByText(/only owner of .*Orphan lab/)).toBeVisible()
    await expect(del.getByRole('button', { name: 'Delete user' })).toHaveCount(0)
    const takeOver = del.getByRole('button', { name: 'Make me the owner and delete' })
    await admin.setViewportSize({ width: 360, height: 740 })
    await expect(takeOver).toBeVisible()
    const box = (await takeOver.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(360)
    // One line of text: a squeezed footer wraps the label instead of clipping it.
    expect(box.height).toBeLessThan(40)
    const clipped = await takeOver.evaluate((el) =>
      [el, ...el.querySelectorAll('*')].some((n) => n.scrollWidth > n.clientWidth),
    )
    expect(clipped).toBe(false)
    await takeOver.click()
    await expect(admin.getByText(`Deleted ${email}`, { exact: true })).toBeVisible()
    await expect(userRow(admin, email)).toHaveCount(0)

    await admin.goto('/admin/projects')
    await expect(userRow(admin, 'Orphan lab')).toContainText('admin@example.test')
  } finally {
    await ctx.close()
    await admin.context().close()
  }
})

test('projects: set a quota, delete an empty project', async ({ browser }) => {
  const admin = await adminPage(browser)
  try {
    await admin.goto('/admin/projects')
    await admin.getByRole('link', { name: 'Orphan lab' }).click()
    await expect(admin.getByRole('heading', { name: 'Orphan lab' })).toBeVisible()
    await admin.getByLabel('Servers limit').fill('7')
    await admin.getByRole('button', { name: 'Save limits' }).click()
    await expect(admin.getByText('Limits saved', { exact: true })).toBeVisible()
    await admin.reload()
    await expect(admin.getByLabel('Servers limit')).toHaveValue('7')

    await admin.goto('/admin/projects')
    await admin.reload()
    await expect(userRow(admin, 'Orphan lab')).toContainText('0 of 7')

    await admin.getByRole('link', { name: 'Orphan lab' }).click()
    await admin.getByRole('button', { name: 'Delete project' }).click()
    const del = admin.getByRole('dialog')
    await del.locator('input[name=confirm_name]').fill('Orphan lab')
    await del.getByRole('button', { name: 'Delete project' }).click()
    await expect(admin).toHaveURL(/\/admin\/projects$/)
    await expect(admin.getByText('Deleted Orphan lab', { exact: true })).toBeVisible()
    await expect(userRow(admin, 'Orphan lab')).toHaveCount(0)
  } finally {
    await admin.context().close()
  }
})

test('activity: a failed create shows technical details to the admin only', async ({ browser }) => {
  const admin = await adminPage(browser)
  const ctx = await browser.newContext()
  try {
    const made = await admin.request.post('/v1/projects', { data: { name: 'Fault lab' } })
    expect(made.status()).toBe(201)
    const projectId = ((await made.json()) as { id: number }).id
    const invited = await admin.request.post(`/v1/projects/${projectId}/invites`, {
      data: { email: 'member@example.test', role: 'member' },
    })
    expect(invited.status()).toBe(201)
    const link = ((await invited.json()) as { invite_url: string }).invite_url

    const member = await ctx.newPage()
    await member.goto(link)
    await member.locator('input[name=name]').fill('Member')
    await member.locator('input[name=password]').fill(PASSWORD)
    await member.getByRole('button', { name: 'Create account and join' }).click()
    await expect(member).toHaveURL(new RegExp(`/p/${projectId}/servers$`))

    const armed = await admin.request.post(harness.fail_next, {
      data: { pattern: '/clone', status: 500 },
    })
    expect(armed.status()).toBe(204)
    await admin.goto(`/p/${projectId}/servers/new`)
    const create = admin.getByRole('button', { name: 'Create server' })
    await expect(create).toBeEnabled()
    await create.click()
    await expect(admin).toHaveURL(/\/p\/\d+\/servers\/\d+$/)

    await admin.goto(`/admin/activity?project=${projectId}`)
    const row = admin.getByRole('listitem').filter({ hasText: 'Fault lab' }).first()
    await expect(row).toContainText('Failed', { timeout: 60_000 })
    await admin
      .getByRole('group', { name: 'Filter by status' })
      .getByRole('button', { name: 'Failed' })
      .click()
    await expect(admin).toHaveURL(/status=error/)
    await expect(admin).toHaveURL(new RegExp(`project=${projectId}`))
    await row.getByRole('button', { name: 'Technical details' }).click()
    await expect(row).toContainText(/qmclone:\d+/)

    await member.goto(`/p/${projectId}/activity`)
    const failed = member.getByRole('listitem').filter({ hasText: 'Failed' }).first()
    await expect(failed).toBeVisible({ timeout: 60_000 })
    await expect(failed.locator('p.text-error')).toBeVisible()
    await expect(member.getByRole('button', { name: 'Technical details' })).toHaveCount(0)
    await expectNoLeaks(member)
  } finally {
    await ctx.close()
    await admin.context().close()
  }
})

test('settings: turning on sign-ups shows the link on the login page', async ({ browser }) => {
  const admin = await adminPage(browser)
  const ctx = await browser.newContext()
  try {
    const off = await admin.request.put('/v1/admin/settings', { data: { self_signup: false } })
    expect(off.ok()).toBeTruthy()
    const visitor = await ctx.newPage()
    await visitor.goto('/login')
    await expect(visitor.getByRole('button', { name: 'Log in', exact: true })).toBeVisible()
    await expect(visitor.getByRole('link', { name: 'Create an account' })).toHaveCount(0)

    await admin.goto('/admin/settings')
    const toggle = admin.getByRole('switch', { name: 'Let anyone create an account' })
    await expect(toggle).not.toBeChecked()
    await toggle.click()
    await expect(admin.getByText('Sign-ups are on', { exact: true })).toBeVisible()
    await expect(toggle).toBeChecked()

    await visitor.reload()
    await expect(visitor.getByRole('link', { name: 'Create an account' })).toBeVisible()
  } finally {
    const off = await admin.request.put('/v1/admin/settings', { data: { self_signup: false } })
    expect(off.ok()).toBeTruthy()
    await ctx.close()
    await admin.context().close()
  }
})
