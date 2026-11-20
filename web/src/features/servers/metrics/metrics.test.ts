import { describe, expect, it } from 'vitest'
import type uPlot from 'uplot'
import { formatPercent, formatRate, readout, toSeries } from './metrics'

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

describe('readout', () => {
  const data = [
    [1763632800, 1763632860],
    [1536, null],
    [0, 2048],
  ] as unknown as uPlot.AlignedData

  it('formats the time and each value at the cursor', () => {
    expect(readout(data, 0, ['Read', 'Write'], formatRate, 'UTC')).toEqual({
      time: '20 Nov, 10:00',
      values: [
        { label: 'Read', value: '1.5 KB/s' },
        { label: 'Write', value: '0 B/s' },
      ],
    })
  })

  it('says no data for a gap', () => {
    expect(readout(data, 1, ['Read', 'Write'], formatRate, 'UTC')?.values).toEqual([
      { label: 'Read', value: 'No data' },
      { label: 'Write', value: '2.0 KB/s' },
    ])
  })

  it('is empty off the plot', () => {
    expect(readout(data, null, ['Read', 'Write'], formatRate)).toBeNull()
    expect(readout(data, 5, ['Read', 'Write'], formatRate)).toBeNull()
  })

  it('formats percent', () => {
    const cpu = [[1763632800], [42.4]] as unknown as uPlot.AlignedData
    expect(readout(cpu, 0, ['CPU'], formatPercent, 'UTC')?.values).toEqual([
      { label: 'CPU', value: '42%' },
    ])
  })
})
