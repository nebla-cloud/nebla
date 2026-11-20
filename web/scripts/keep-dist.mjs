// Vite empties dist on build; restore the tracked .gitkeep so the Go embed and git status stay clean.
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'

writeFileSync(join(import.meta.dirname, '..', 'dist', '.gitkeep'), '')
