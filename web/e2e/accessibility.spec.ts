import { AxeBuilder } from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { adminPage, adminState, createServer } from './helpers'

test('core pages have no serious or critical accessibility violations', async ({ browser }) => {
  const seed = await adminPage(browser)
  let serverName: string
  let detail: string
  try {
    await seed.goto('/projects')
    await seed.getByRole('link', { name: /Home lab/ }).click()
    serverName = await createServer(seed)
    detail = new URL(seed.url()).pathname
  } finally {
    await seed.context().close()
  }

  const servers = detail.replace(/\/\d+$/, '')
  const pages = [
    { path: servers, heading: 'Servers', ready: serverName },
    { path: detail, heading: serverName, ready: 'Recent activity' },
    { path: `${servers}/new`, heading: 'Create server', ready: 'Cloud-init' },
    { path: '/admin/locations', heading: 'Locations', ready: 'home' },
  ]

  for (const colorScheme of ['light', 'dark'] as const) {
    const context = await browser.newContext({ colorScheme, storageState: adminState() })
    try {
      const page = await context.newPage()
      for (const viewport of [
        { width: 1440, height: 1000 },
        { width: 390, height: 844 },
      ]) {
        await page.setViewportSize(viewport)
        for (const { path, heading, ready } of pages) {
          await test.step(`${path} at ${viewport.width} in ${colorScheme}`, async () => {
            await page.goto(path)
            await expect(
              page.getByRole('heading', { name: heading, level: 1, exact: true }),
            ).toBeVisible()
            await expect(page.getByText(ready, { exact: true }).first()).toBeVisible()
            const html = expect(page.locator('html'))
            if (colorScheme === 'dark') await html.toHaveClass(/\bdark\b/)
            else await html.not.toHaveClass(/\bdark\b/)
            const { violations } = await new AxeBuilder({ page }).analyze()
            const major = violations
              .filter(({ impact }) => impact === 'serious' || impact === 'critical')
              .map(({ id, nodes }) => ({ id, targets: nodes.map(({ target }) => target) }))
            expect.soft(major).toEqual([])
          })
        }
      }
    } finally {
      await context.close()
    }
  }
})
