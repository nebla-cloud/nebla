import { describe, expect, it } from 'vitest'
import * as ui from './ui'
import { selectClass, table, tableWrap, tbody, thead } from './ui'

const tokens = (s: string) => s.split(/\s+/).filter(Boolean)

describe('ui classes', () => {
  it('keeps the stack-on-mobile table pattern', () => {
    expect(tokens(table)).toEqual(expect.arrayContaining(['block', 'sm:table']))
    expect(tokens(thead)).toEqual(
      expect.arrayContaining(['hidden', 'bg-(--nb-soft)', 'text-muted', 'sm:table-header-group']),
    )
    expect(tokens(tbody)).toContain('sm:table-row-group')
    expect(tokens(tableWrap)).toContain('overflow-hidden')
  })

  it('has one select style on the input border', () => {
    expect(tokens(selectClass)).toEqual(expect.arrayContaining(['border-default', 'h-9']))
  })

  it('never glues two utilities together', () => {
    for (const [name, value] of Object.entries(ui)) {
      for (const t of tokens(value as string)) {
        expect(t, `${name}: ${t}`).not.toMatch(/\)[A-Za-z]/)
      }
    }
  })
})
