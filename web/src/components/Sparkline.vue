<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<script setup lang="ts">
import { computed, useId } from 'vue'
import { area, curveMonotoneX, line } from 'd3-shape'

type Tone = 'busy' | 'run' | 'err'
const TONE_VAR: Record<Tone, string> = { busy: '--nb-busy', run: '--nb-run', err: '--nb-err' }

const props = withDefaults(
  defineProps<{
    lines: { points: [number, number | null][]; tone: Tone }[]
    width: number
    height: number
    // Fixed top of the scale; by default the peak across every line.
    max?: number
    strokeWidth?: number
  }>(),
  { max: undefined, strokeWidth: 1.5 },
)

const uid = useId()

const paths = computed(() => {
  const sets = props.lines.map((l) => ({
    tone: l.tone,
    pts: l.points.filter((p): p is [number, number] => p[1] != null),
  }))
  const all = sets.flatMap((s) => s.pts)
  if (!all.length) return []
  const t0 = Math.min(...all.map((p) => p[0]))
  const span = Math.max(...all.map((p) => p[0])) - t0 || 1
  const max = props.max ?? (Math.max(...all.map((p) => p[1]), 0) || 1)
  const h = props.height
  const x = (p: [number, number]) => +(((p[0] - t0) / span) * props.width).toFixed(2)
  const y = (p: [number, number]) => +(h - (Math.min(Math.max(0, p[1]), max) / max) * h).toFixed(2)
  const stroke = line<[number, number]>().x(x).y(y).curve(curveMonotoneX)
  const fill = area<[number, number]>().x(x).y1(y).y0(h).curve(curveMonotoneX)
  return sets.flatMap((s, i) =>
    s.pts.length < 2
      ? []
      : [
          {
            key: i,
            id: `${uid}-g${i}`,
            color: `var(${TONE_VAR[s.tone]})`,
            line: stroke(s.pts)!,
            area: fill(s.pts)!,
          },
        ],
  )
})
</script>

<template>
  <svg
    aria-hidden="true"
    :viewBox="`0 0 ${width} ${height}`"
    preserveAspectRatio="none"
    class="block w-full"
    :style="{ height: `${height}px` }"
  >
    <defs>
      <linearGradient v-for="p in paths" :id="p.id" :key="p.key" x1="0" x2="0" y1="0" y2="1">
        <stop offset="0" :style="`stop-color: ${p.color}; stop-opacity: 0.22`" />
        <stop offset="1" :style="`stop-color: ${p.color}; stop-opacity: 0`" />
      </linearGradient>
    </defs>
    <template v-for="p in paths" :key="p.key">
      <path data-area :d="p.area" :fill="`url(#${p.id})`" />
      <path
        data-line
        :d="p.line"
        fill="none"
        :stroke="p.color"
        :stroke-width="strokeWidth"
        stroke-linejoin="round"
        stroke-linecap="round"
        vector-effect="non-scaling-stroke"
      />
    </template>
  </svg>
</template>
