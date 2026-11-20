import { describe, expect, it } from 'vitest'
import { formatPercent, formatRate, toSeries } from './metrics'

describe('toSeries', () => {
  it('nulls stay null', () => {
    expect(
      toSeries(
        {
          cpu: [
            [1, 5],
            [2, null],
            [3, 7],
          ],
        },
        ['cpu'],
      ),
    ).toEqual([
      [1, 2, 3],
      [5, null, 7],
    ])
  })

  it('aligns series with different timestamps', () => {
    const out = toSeries(
      {
        'disk.read': [
          [1, 1],
          [3, 3],
        ],
        'disk.write': [
          [2, 20],
          [3, 30],
        ],
      },
      ['disk.read', 'disk.write'],
    )
    expect(out).toEqual([
      [1, 2, 3],
      [1, null, 3],
      [null, 20, 30],
    ])
  })

  it('missing key gives an all-null column', () => {
    expect(toSeries({ a: [[1, 1]] }, ['a', 'b'])).toEqual([[1], [1], [null]])
  })
})

describe('formatRate', () => {
  it('units', () => {
    expect(formatRate(0)).toBe('0 B/s')
    expect(formatRate(512)).toBe('512 B/s')
    expect(formatRate(1.2 * 1024)).toBe('1.2 KB/s')
    expect(formatRate(3.4 * 1024 * 1024)).toBe('3.4 MB/s')
    expect(formatRate(1.1 * 1024 ** 3)).toBe('1.1 GB/s')
  })
})

describe('formatPercent', () => {
  it('rounds', () => {
    expect(formatPercent(42.4)).toBe('42%')
    expect(formatPercent(42.6)).toBe('43%')
  })
})
