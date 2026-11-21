import { describe, expect, it } from 'vitest'
import {
  chartHeader,
  formatPercent,
  formatAxisRate,
  formatRate,
  formatTick,
  nearestIndex,
  segments,
  timeTicks,
  toRows,
  yDomain,
  yTicks,
} from './metrics'

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

describe('yDomain rounds up to a tick', () => {
  it('rate peaks land on a round step', () => {
    expect(yDomain([{ t: 0, v: [400 * 1024] }], 'rate')).toEqual([0, 400 * 1024])
    expect(yDomain([{ t: 0, v: [447 * 1024] }], 'rate')).toEqual([0, 500 * 1024])
  })
})

describe('yTicks', () => {
  it('percent ticks every 25', () => expect(yTicks(100, 'percent')).toEqual([25, 50, 75, 100]))
  it('idle rate ticks every 256 B/s', () =>
    expect(yTicks(1024, 'rate')).toEqual([256, 512, 768, 1024]))
  it('rate ticks are round in their unit', () =>
    expect(yTicks(500 * 1024, 'rate').map(formatRate)).toEqual([
      '100 KB/s',
      '200 KB/s',
      '300 KB/s',
      '400 KB/s',
      '500 KB/s',
    ]))
})

describe('timeTicks', () => {
  it('an hour ticks every 15 minutes on the quarter', () => {
    const t0 = Date.UTC(2026, 10, 20, 15, 3) / 1000
    const ticks = timeTicks(t0, t0 + 3540, 'UTC')
    // 16:00 is two minutes from the end, too close to the edge for its label.
    expect(ticks.map((t) => formatTick(t, 3540, 'UTC'))).toEqual(['15:15', '15:30', '15:45'])
  })
  it('a week ticks on day boundaries', () => {
    const t0 = Date.UTC(2026, 10, 14, 10) / 1000
    const ticks = timeTicks(t0, t0 + 7 * 86400, 'UTC')
    expect(ticks.length).toBeGreaterThanOrEqual(3)
    expect(ticks.length).toBeLessThanOrEqual(7)
    for (const t of ticks) expect(t % 86400).toBe(0)
    expect(formatTick(ticks[0]!, 7 * 86400, 'UTC')).toMatch(/^\d{1,2} Nov$/)
  })
  it('drops a tick on the very first sample', () => {
    const t0 = Date.UTC(2026, 10, 20, 15, 0) / 1000
    expect(timeTicks(t0, t0 + 3600, 'UTC')[0]).toBe(t0 + 900)
  })
  it('day ticks stay on local midnight across a clock change', () => {
    const t0 = Date.UTC(2026, 9, 21, 12) / 1000 // a week ending after the 25 Oct change
    const ticks = timeTicks(t0, t0 + 7 * 86400, 'Europe/London')
    const time = new Intl.DateTimeFormat('en-GB', {
      timeZone: 'Europe/London',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    })
    expect(ticks.length).toBeGreaterThan(0)
    for (const t of ticks) expect(time.format(t * 1000)).toBe('00:00')
  })
  it('no span gives no ticks', () => expect(timeTicks(5, 5)).toEqual([]))
})

describe('segments', () => {
  const rows = [0, 1, 2, 3, 4].map((t) => ({ t: t * 10, v: [t === 2 ? null : 1] }))
  it('spans of present samples as fractions of the time range', () => {
    expect(segments(rows, 0)).toEqual([
      [0, 0.25],
      [0.75, 1],
    ])
  })
  it('no gaps is one full span', () => {
    expect(segments([{ t: 0, v: [1] }, { t: 10, v: [2] }], 0)).toEqual([[0, 1]])
  })
})

describe('formatAxisRate', () => {
  it('labels every tick in the top tick\'s unit', () => {
    const max = 1.25 * 1024 * 1024
    expect(yTicks(max, 'rate').map((v) => formatAxisRate(v, max))).toEqual([
      '0.25 MB/s',
      '0.5 MB/s',
      '0.75 MB/s',
      '1 MB/s',
      '1.25 MB/s',
    ])
  })
  it('whole units stay whole', () => {
    expect(formatAxisRate(300 * 1024, 500 * 1024)).toBe('300 KB/s')
  })
})
