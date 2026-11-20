import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseProjectId } from './projectId'

describe('projectId', () => {
  it('has no app imports', () => {
    const src = readFileSync('src/lib/projectId.ts', 'utf8')
    expect(src).not.toMatch(/from\s+['"]@\//)
  })

  it('parses positive integers only', () => {
    expect(parseProjectId('12')).toBe(12)
    expect(parseProjectId('0')).toBeNull()
    expect(parseProjectId(undefined)).toBeNull()
  })
})
