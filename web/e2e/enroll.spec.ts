import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { login } from './helpers'

const harness = JSON.parse(process.env.NEBLA_E2E_HARNESS ?? '{}') as {
  pve: { url: string; ca_fingerprint: string }
  add_token: string
}

test('enroll paste-back connects a location', async ({ page, request }) => {
  // Earlier specs drain the admin's API bucket (refills 1/s); let it recover.
  await page.waitForTimeout(30_000)
  await login(page)
  await page.goto('/admin/locations')
  await page.getByRole('button', { name: 'Connect a location' }).first().click()
  await page.locator('input[name=name]').fill('lab')
  await page.getByRole('button', { name: 'Get setup command' }).click()
  await expect(page).toHaveURL(/\/admin\/locations\/enroll/)

  await page.getByRole('button', { name: "The node can't reach this panel?" }).click()
  const script = (await page.getByTestId('enroll-offline').textContent()) ?? ''
  const token = /NEBLA_TOKEN=(nbe_[\w-]{43})/.exec(script)![1]
  const name = /NEBLA_TOKEN_NAME=(nebla-lab-[a-z0-9]{6})/.exec(script)![1]

  const secret = randomUUID()
  const added = await request.post(harness.add_token, {
    data: { id: `nebla@pve!${name}`, secret },
  })
  expect(added.status()).toBe(204)

  const ip = new URL(harness.pve.url).hostname
  const report = {
    v: 1,
    pve_version: '9.0.3',
    node: 'pve1',
    token: { 'full-tokenid': `nebla@pve!${name}`, value: secret, info: {} },
    cluster: ['pve1', 'pve2'].map((n) => ({ type: 'node', name: n, ip, online: 1, local: 0 })),
    storage: [
      { storage: 'ceph', type: 'rbd', content: 'images', shared: 1 },
      { storage: 'local', type: 'dir', content: 'iso', shared: 0 },
      { storage: 'imp', type: 'dir', content: 'import', shared: 0 },
      { storage: 'spare', type: 'dir', content: 'iso', shared: 0 },
    ],
    fingerprints: { pve1: harness.pve.ca_fingerprint, pve2: harness.pve.ca_fingerprint },
    nebla_iso: ['local', 'imp', 'spare'],
    enroll_token: token,
  }
  const blob = 'nebla-enroll:v1:' + Buffer.from(JSON.stringify(report)).toString('base64url')

  await page.locator('textarea[name=blob]').fill(blob)
  await page.getByRole('button', { name: 'Submit' }).click()

  const finish = page.getByTestId('enroll-finish')
  await expect(finish).toBeVisible()
  await finish.locator('input[name=url]').fill(harness.pve.url)
  await finish.locator('select[name=disk_storage]').selectOption('ceph')
  await finish.locator('select[name=iso_storage]').selectOption('local')
  await finish.locator('select[name=import_storage]').selectOption('imp')
  await finish.getByRole('button', { name: 'Connect location' }).click()

  await expect(page).toHaveURL(/\/admin\/locations\/\d+$/)
  await expect(page.getByTestId('enroll-cleanup')).toContainText('/storage/spare')
  expect(await page.content()).not.toContain(secret)
})
