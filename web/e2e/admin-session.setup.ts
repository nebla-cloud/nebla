import { test } from '@playwright/test'
import { adminState, login } from './helpers'

test('admin session', async ({ page }) => {
  await login(page)
  await page.context().storageState({ path: adminState() })
})
