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
  formatPercent,
  formatRate,
  formatTime,
  nearestIndex,
  yDomain,
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
const uid = useId()
const me = Symbol(props.title)
const hover = useHoverSync(props.syncKey)

const fmt = (v: number) => (props.unit === 'percent' ? formatPercent(v) : formatRate(v))
const color = (i: number) => `var(${TONE_VAR[props.series[i]!.tone]})`
const gradId = (i: number) => `${uid}-g${i}`

const svgDefs = computed(() =>
  props.series
    .map(
      (_, i) =>
        `<linearGradient id="${gradId(i)}" x1="0" x2="0" y1="0" y2="1">` +
        `<stop offset="0" style="stop-color: ${color(i)}; stop-opacity: 0.22"></stop>` +
        `<stop offset="1" style="stop-color: ${color(i)}; stop-opacity: 0"></stop>` +
        `</linearGradient>`,
    )
    .join(''),
)

const x = (r: MetricRow) => r.t
const areaY = (i: number) => (r: MetricRow) => r.v[i] ?? undefined
const lineY = computed(() => props.series.map((_, i) => areaY(i)))
const lineColor = (_: unknown, i: number) => color(i)

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

const xTick = (t: number | Date) => formatTime(+t)
const yTick = (v: number | Date) => (+v === 0 ? '' : fmt(+v))
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
      class="nb-chart w-full"
      role="img"
      :aria-label="`${title} over time`"
      @mouseleave="release"
    >
      <VisXYContainer
        :data="rows"
        :height="HEIGHT"
        :yDomain="yDomain(rows, unit)"
        :svgDefs="svgDefs"
        :margin="{ top: 8 }"
      >
        <VisArea
          v-for="(s, i) in series"
          :key="s.label"
          :x="x"
          :y="areaY(i)"
          :color="`url(#${gradId(i)})`"
          :curveType="CurveType.MonotoneX"
        />
        <VisLine
          :x="x"
          :y="lineY"
          :color="lineColor"
          :lineWidth="2"
          :curveType="CurveType.MonotoneX"
        />
        <VisAxis
          type="x"
          :tickFormat="xTick"
          :numTicks="5"
          :gridLine="false"
          :tickLine="false"
        />
        <VisAxis
          type="y"
          :tickFormat="yTick"
          :numTicks="4"
          :domainLine="false"
          :tickLine="false"
        />
        <VisCrosshair
          :template="template"
          :onCrosshairMove="onCrosshairMove"
          :forceShowAt="following ? (hover.t ?? undefined) : undefined"
        />
        <VisTooltip />
      </VisXYContainer>
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
