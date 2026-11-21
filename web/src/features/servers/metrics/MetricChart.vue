<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useColorMode } from '@vueuse/core'
import type uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import Panel from '@/components/Panel.vue'
import {
  TONE_VAR,
  chartColors,
  formatPercent,
  formatRate,
  readout,
  type Readout,
  type Tone,
} from './metrics'

const props = defineProps<{
  title: string
  data: uPlot.AlignedData
  series: { label: string; tone: Tone }[]
  unit: 'percent' | 'rate'
}>()

const SYNC_KEY = 'server-metrics'
const HEIGHT = 180
const el = ref<HTMLDivElement>()
const mode = useColorMode()
const hover = ref<Readout | null>(null)
let chart: uPlot | null = null
let observer: ResizeObserver | null = null

const fmt = (v: number) => (props.unit === 'percent' ? formatPercent(v) : formatRate(v))

// uPlot touches matchMedia on import, so load it only when a chart mounts.
async function build() {
  if (!el.value) return
  const { default: UPlot } = await import('uplot')
  if (!el.value) return
  chart?.destroy()
  // Read after the await, so a colour-mode switch has reached <html>.
  const c = chartColors()
  const axisBase = { stroke: c.axis, grid: { stroke: c.grid, width: 1 }, ticks: { stroke: c.grid } }
  chart = new UPlot(
    {
      width: el.value.clientWidth || 300,
      height: HEIGHT,
      legend: { show: false },
      cursor: { sync: { key: SYNC_KEY } },
      // Synced charts fire this too, so all three read out the same moment.
      hooks: {
        setCursor: [
          (u) => {
            hover.value = readout(
              u.data,
              u.cursor.idx,
              props.series.map((s) => s.label),
              fmt,
            )
          },
        ],
      },
      scales: {
        y: {
          range: props.unit === 'percent' ? [0, 100] : (_u, _min, max) => [0, Math.max(max, 1024)],
        },
      },
      axes: [axisBase, { ...axisBase, size: 70, values: (_u, vals) => vals.map((v) => fmt(v)) }],
      series: [
        {},
        ...props.series.map((s) => ({
          label: s.label,
          stroke: c.tone[s.tone],
          width: 2,
          spanGaps: false,
          points: { show: false },
        })),
      ],
    },
    props.data,
    el.value,
  )
}

onMounted(() => {
  void build()
  observer = new ResizeObserver(() => {
    if (chart && el.value) chart.setSize({ width: el.value.clientWidth, height: HEIGHT })
  })
  if (el.value) observer.observe(el.value)
})

watch(
  () => props.data,
  (d) => chart?.setData(d),
)
watch(mode, () => void build(), { flush: 'post' })

onBeforeUnmount(() => {
  observer?.disconnect()
  chart?.destroy()
  chart = null
  hover.value = null
})
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
    <p class="mb-1 flex min-h-5 flex-wrap gap-x-3 text-xs tabular-nums">
      <template v-if="hover">
        <time class="text-muted">{{ hover.time }}</time>
        <span v-for="v in hover.values" :key="v.label" class="text-default">
          <template v-if="series.length > 1">{{ v.label }} </template>{{ v.value }}
        </span>
      </template>
    </p>
    <div ref="el" class="w-full" role="img" :aria-label="`${title} over time`" />
  </Panel>
</template>
