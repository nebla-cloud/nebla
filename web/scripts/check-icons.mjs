// Fails the build if an i-lucide-* icon named in src isn't in the client bundle.
// A missing icon would render blank, since the Iconify API fallback is switched off.
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import process from 'node:process'

const web = join(import.meta.dirname, '..')
const used = new Set()
for (const f of readdirSync(join(web, 'src'), { recursive: true })) {
  if (!/\.(ts|vue)$/.test(f) || /\.test\.ts$/.test(f)) continue
  for (const m of readFileSync(join(web, 'src', f), 'utf8').matchAll(/i-lucide-([a-z0-9]+(?:-[a-z0-9]+)*)/g))
    used.add(m[1])
}
const assets = join(web, 'dist', 'assets')
const bundle = readdirSync(assets)
  .filter((f) => f.endsWith('.js'))
  .map((f) => readFileSync(join(assets, f), 'utf8'))
  .join('\n')
const missing = [...used].filter((n) => !bundle.includes(`"${n}":{`)).sort()
if (missing.length) {
  process.stderr.write(`Icons used but not bundled: ${missing.join(', ')}\n`)
  process.exit(1)
}
process.stdout.write(`check-icons: ${used.size} icons bundled\n`)
