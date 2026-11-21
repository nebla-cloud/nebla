import { describe, expect, it } from 'vitest'
import { selectClass, table, tableWrap, tbody, thead } from './ui'

describe('ui classes', () => {
  it('keeps the stack-on-mobile table pattern', () => {
    expect(table).toContain('block')
    expect(table).toContain('sm:table')
    expect(thead).toContain('hidden')
    expect(tbody).toContain('sm:table-row-group')
    expect(tableWrap).toContain('overflow-hidden')
  })

  it('has one select style on the input border', () => {
    expect(selectClass).toContain('border-default')
    expect(selectClass).toContain('h-9')
  })
})
