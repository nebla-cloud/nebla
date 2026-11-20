import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

// ?raw comes back empty under vitest's css handling, so read the file directly.
const css = readFileSync('src/assets/main.css', 'utf8')

function block(selector: string): string {
  const start = css.indexOf(`\n${selector} {`)
  if (start < 0) return ''
  return css.slice(start, css.indexOf('\n}', start))
}

function hexOf(scope: string, name: string): string {
  const m = block(scope).match(new RegExp(`${name}:\\s*(#[0-9A-Fa-f]{6})\\b`))
  if (!m) throw new Error(`${name} has no hex value in ${scope}`)
  return m[1]!
}

function lum(hex: string): number {
  const ch = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * ch[0]! + 0.7152 * ch[1]! + 0.0722 * ch[2]!
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi! + 0.05) / (lo! + 0.05)
}

const modes = [
  { scope: ':root', surfaces: ['#FBFCFD', '#FFFFFF'] },
  { scope: '.dark', surfaces: ['#14171C', '#1A1E25'] },
]

describe('design tokens', () => {
  for (const { scope, surfaces } of modes) {
    describe(scope, () => {
      for (const name of ['--nb-run', '--nb-busy', '--nb-err', '--nb-off']) {
        it(`${name} is at least 3:1`, () => {
          for (const bg of surfaces) expect(contrast(hexOf(scope, name), bg)).toBeGreaterThanOrEqual(3)
        })
      }
      it('muted text is at least 4.5:1', () => {
        for (const bg of surfaces) expect(contrast(hexOf(scope, '--ui-text-muted'), bg)).toBeGreaterThanOrEqual(4.5)
      })
      it('badge text is at least 4.5:1 on its badge', () => {
        const bg = hexOf(scope, '--nb-run-bg')
        expect(contrast(hexOf(scope, '--nb-run-fg'), bg)).toBeGreaterThanOrEqual(4.5)
      })
      it('busy text is at least 4.5:1 on the surfaces', () => {
        for (const bg of surfaces) expect(contrast(hexOf(scope, '--nb-busy-fg'), bg)).toBeGreaterThanOrEqual(4.5)
      })
      it('the focus colour is at least 3:1 on the surfaces', () => {
        for (const bg of surfaces) expect(contrast(hexOf(scope, '--nb-focus'), bg)).toBeGreaterThanOrEqual(3)
      })
    })
  }

  it('defines the shared variables and utilities', () => {
    for (const v of ['--nb-sidebar', '--nb-soft', '--nb-gradient', '--nb-ring', '--nb-radius-panel', '--font-mono', '--nb-ink-active']) {
      expect(css).toContain(`${v}:`)
    }
    for (const u of ['.nb-h1', '.nb-h2', '.nb-mono', '.nb-gradient-underline']) expect(css).toContain(u)
  })

  it('has no glow', () => {
    expect(css).not.toContain('aurora-glow')
    expect(css).not.toContain('--glow-')
  })
})
