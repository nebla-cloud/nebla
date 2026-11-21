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
import { computed, onBeforeUnmount, useId } from 'vue'
import VisXYContainer from '@unovis/vue/containers/xy-container'
import VisArea from '@unovis/vue/components/area'
import VisLine from '@unovis/vue/components/line'
import VisAxis from '@unovis/vue/components/axis'
import VisCrosshair from '@unovis/vue/components/crosshair'
import VisTooltip from '@unovis/vue/components/tooltip'
import { CurveType } from '@unovis/ts/types/curve'
import Panel from '@/components/Panel.vue'
import { useHoverSync } from './hoverSync'
import {
  TONE_VAR,
  chartHeader,
  formatAxisRate,
  formatPercent,
  formatRate,
  formatTick,
  formatTime,
  nearestIndex,
  segments,
  timeTicks,
  yDomain,
  yTicks,
  type MetricRow,
  type Tone,
} from './metrics'

const props = withDefaults(
  defineProps<{
    title: string
    rows: MetricRow[]
    series: { label: string; tone: Tone }[]
    unit: 'percent' | 'rate'
    syncKey?: string
  }>(),
  { syncKey: 'server-metrics' },
)

const HEIGHT = 180
// Fixed margins so the y labels can be laid over the plot at known heights.
const MARGIN = { top: 16, bottom: 22, left: 0, right: 0 }
const PLOT_H = HEIGHT - MARGIN.top - MARGIN.bottom
const uid = useId()
const me = Symbol(props.title)
const hover = useHoverSync(props.syncKey)

const fmt = (v: number) => (props.unit === 'percent' ? formatPercent(v) : formatRate(v))
const color = (i: number) => `var(${TONE_VAR[props.series[i]!.tone]})`
const gradId = (i: number) => `${uid}-g${i}`
const fillId = (i: number) => `${uid}-f${i}`

// Unovis draws a missing sample as 0, so the fill is a pattern that only paints the
// stretches with data: gaps stay empty instead of sloping down to the baseline.
const svgDefs = computed(() =>
  props.series
    .map((_, i) => {
      const rects = segments(props.rows, i)
        .map(([a, b]) => `<rect x="${a}" y="0" width="${b - a}" height="1" fill="url(#${gradId(i)})"></rect>`)
        .join('')
      return (
        `<linearGradient id="${gradId(i)}" x1="0" x2="0" y1="0" y2="1">` +
        `<stop offset="0" style="stop-color: ${color(i)}; stop-opacity: 0.22"></stop>` +
        `<stop offset="1" style="stop-color: ${color(i)}; stop-opacity: 0"></stop>` +
        `</linearGradient>` +
        `<pattern id="${fillId(i)}" patternUnits="objectBoundingBox" patternContentUnits="objectBoundingBox" width="1" height="1">${rects}</pattern>`
      )
    })
    .join(''),
)
const domain = computed(() => yDomain(props.rows, props.unit))
const yValues = computed(() => yTicks(domain.value[1], props.unit))
const yLabels = computed(() =>
  yValues.value.map((v) => ({
    text: props.unit === 'percent' ? formatPercent(v) : formatAxisRate(v, domain.value[1]),
    top: MARGIN.top + (1 - v / domain.value[1]) * PLOT_H,
  })),
)
const xValues = computed(() =>
  props.rows.length ? timeTicks(props.rows[0]!.t, props.rows.at(-1)!.t) : [],
)
const span = computed(() => (props.rows.length ? props.rows.at(-1)!.t - props.rows[0]!.t : 0))

const x = (r: MetricRow) => r.t
const areaY = (i: number) => (r: MetricRow) => r.v[i] ?? undefined
// Built once per series list: Unovis compares accessors by reference and redraws on a new one.
const ys = computed(() => props.series.map((_, i) => areaY(i)))
const lineColor = (_: unknown, i: number) => color(i)
// The crosshair counts every accessor: the areas first, then the line's series.
const dotColor = (_: unknown, i: number) => color(i % props.series.length)

const following = computed(() => hover.value.source !== null && hover.value.source !== me)
const hoverIdx = computed(() =>
  hover.value.t == null ? null : nearestIndex(props.rows, hover.value.t),
)
const head = computed(() =>
  chartHeader(
    props.rows,
    props.series.map((s) => s.label),
    props.unit,
    hoverIdx.value != null && hoverIdx.value >= 0 ? hoverIdx.value : null,
  ),
)

function onCrosshairMove(t?: number | Date) {
  if (t === undefined) release()
  else hover.value = { t: +t, source: me }
}
function release() {
  if (hover.value.source === me) hover.value = { t: null, source: null }
}
onBeforeUnmount(release)

// Charts that follow another one show only the line, no tooltip.
function template(d: MetricRow) {
  if (following.value || !d) return ''
  const values = props.series
    .map((s, i) => {
      const v = d.v[i]
      return `<div><span style="display:inline-block;width:8px;height:8px;border-radius:9999px;margin-right:6px;background:${color(i)}"></span>${s.label} <b>${v == null ? 'No data' : fmt(v)}</b></div>`
    })
    .join('')
  return `<div style="color:var(--ui-text-muted);font-size:11px">${formatTime(d.t)}</div>${values}`
}

const xTick = (t: number | Date) => formatTick(+t, span.value)
const noLabel = () => ''
</script>

<template>
  <Panel :title="title">
    <template v-if="series.length > 1" #header-actions>
      <ul class="flex gap-3 text-xs text-muted">
        <li v-for="s in series" :key="s.label" class="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            class="inline-block size-2 rounded-full"
            :style="{ background: `var(${TONE_VAR[s.tone]})` }"
          />
          {{ s.label }}
        </li>
      </ul>
    </template>
    <div class="mb-2 flex flex-wrap items-end justify-between gap-x-4 gap-y-1">
      <p class="text-highlighted">
        <span data-value class="text-2xl font-bold tabular-nums">{{ head.value }}</span>
        <span v-if="head.unit" class="ml-1 text-sm text-muted">{{ head.unit }}</span>
      </p>
      <p data-stats class="flex gap-3 text-xs text-muted tabular-nums">
        <span v-for="s in head.stats" :key="s">{{ s }}</span>
      </p>
    </div>
    <div
      class="nb-chart relative w-full"
      role="img"
      :aria-label="`${title} over time`"
      @mouseleave="release"
    >
      <VisXYContainer
        :data="rows"
        :height="HEIGHT"
        :yDomain="domain"
        :svgDefs="svgDefs"
        :margin="MARGIN"
        :autoMargin="false"
      >
        <VisArea
          v-for="(s, i) in series"
          :key="s.label"
          :x="x"
          :y="ys[i]"
          :color="`url(#${fillId(i)})`"
          :curveType="CurveType.MonotoneX"
        />
        <VisLine
          :x="x"
          :y="ys"
          :color="lineColor"
          :lineWidth="2"
          :curveType="CurveType.MonotoneX"
        />
        <VisAxis
          type="x"
          :tickFormat="xTick"
          :tickValues="xValues"
          :tickPadding="6"
          :gridLine="false"
          :tickLine="false"
        />
        <VisAxis
          type="y"
          :tickFormat="noLabel"
          :tickValues="yValues"
          :tickPadding="0"
          :domainLine="false"
          :tickLine="false"
        />
        <VisCrosshair
          :color="dotColor"
          :template="template"
          :onCrosshairMove="onCrosshairMove"
          :forceShowAt="following ? (hover.t ?? undefined) : undefined"
        />
        <VisTooltip />
      </VisXYContainer>
      <span
        v-for="l in yLabels"
        :key="l.text"
        data-y-label
        class="pointer-events-none absolute left-0 -translate-y-full pb-0.5 text-[10px] leading-none text-muted tabular-nums"
        :style="{ top: `${l.top}px` }"
        >{{ l.text }}</span
      >
    </div>
  </Panel>
</template>

<style scoped>
/* Unovis reads its colours from these variables, so dark mode follows the tokens. */
.nb-chart {
  --vis-font-family: var(--font-sans);
  --vis-axis-tick-label-color: var(--ui-text-muted);
  --vis-axis-tick-label-font-size: 10px;
  --vis-axis-grid-color: var(--ui-border);
  --vis-axis-domain-color: var(--ui-border);
  --vis-axis-tick-color: transparent;
  --vis-crosshair-line-stroke-color: var(--ui-text-muted);
  --vis-tooltip-background-color: var(--ui-bg-elevated);
  --vis-tooltip-border-color: var(--ui-border);
  --vis-tooltip-text-color: var(--ui-text);
  --vis-tooltip-border-radius: 8px;
}
</style>
