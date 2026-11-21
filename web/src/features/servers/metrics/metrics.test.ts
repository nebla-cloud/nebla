import { describe, expect, it } from 'vitest'
import type uPlot from 'uplot'
import {
  chartHeader,
  formatPercent,
  formatRate,
  nearestIndex,
  readout,
  toRows,
  toSeries,
  yDomain,
} from './metrics'

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

describe('toRows', () => {
  it('unions timestamps and keeps nulls', () => {
    const rows = toRows(
      {
        a: [
          [1, 5],
          [3, null],
        ],
        b: [[2, 7]],
      },
      ['a', 'b'],
    )
    expect(rows).toEqual([
      { t: 1, v: [5, null] },
      { t: 2, v: [null, 7] },
      { t: 3, v: [null, null] },
    ])
  })

  it('missing key is all null', () => {
    expect(toRows({ a: [[1, 2]] }, ['a', 'zz'])).toEqual([{ t: 1, v: [2, null] }])
  })
})

describe('nearestIndex', () => {
  const rows = [
    { t: 0, v: [] },
    { t: 60, v: [] },
    { t: 120, v: [] },
  ]

  it('snaps to the closest row', () => {
    expect(nearestIndex(rows, 25)).toBe(0)
    expect(nearestIndex(rows, 35)).toBe(1)
    expect(nearestIndex(rows, 999)).toBe(2)
  })

  it('is -1 for no rows', () => expect(nearestIndex([], 5)).toBe(-1))
})

describe('yDomain', () => {
  it('percent is fixed', () => expect(yDomain([{ t: 0, v: [250] }], 'percent')).toEqual([0, 100]))

  it('idle rate floors at 1 KB/s', () =>
    expect(yDomain([{ t: 0, v: [0, null] }], 'rate')).toEqual([0, 1024]))

  it('rate uses the peak across series', () =>
    expect(yDomain([{ t: 0, v: [10, 4096] }], 'rate')).toEqual([0, 4096]))
})

describe('chartHeader', () => {
  const cpu = [
    { t: 0, v: [7] },
    { t: 60, v: [78] },
    { t: 120, v: [14] },
    { t: 180, v: [null] },
  ]

  it('single series: latest value and min / avg / max', () => {
    expect(chartHeader(cpu, ['CPU'], 'percent', null)).toEqual({
      value: '14%',
      stats: ['min 7%', 'avg 33%', 'max 78%'],
    })
  })

  it('follows the hovered index', () => {
    expect(chartHeader(cpu, ['CPU'], 'percent', 1).value).toBe('78%')
  })

  it('two series: first big, second in the unit slot, peak per series', () => {
    const disk = [
      { t: 0, v: [1536, 0] },
      { t: 60, v: [4096, null] },
    ]
    expect(chartHeader(disk, ['Read', 'Write'], 'rate', null)).toEqual({
      value: '4.0 KB/s',
      unit: 'read / 0 B/s write',
      stats: ['peak read 4.0 KB/s', 'peak write 0 B/s'],
    })
  })

  it('a series with no data shows a dash', () => {
    expect(chartHeader([{ t: 0, v: [5, null] }], ['In', 'Out'], 'rate', null)).toEqual({
      value: '5 B/s',
      unit: 'in / — out',
      stats: ['peak in 5 B/s', 'peak out —'],
    })
  })

  it('hovering a gap shows a dash', () => {
    expect(chartHeader(cpu, ['CPU'], 'percent', 3).value).toBe('—')
  })
})
