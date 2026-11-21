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
