<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useColorMode } from '@vueuse/core'
import type uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { formatPercent, formatRate, readout, type Readout } from './metrics'

const props = defineProps<{
  title: string
  data: uPlot.AlignedData
  series: { label: string; tone: 'primary' | 'secondary' }[]
  unit: 'percent' | 'rate'
}>()

const SYNC_KEY = 'server-metrics'
const HEIGHT = 180
const el = ref<HTMLDivElement>()
const mode = useColorMode()
const hover = ref<Readout | null>(null)
let chart: uPlot | null = null
let observer: ResizeObserver | null = null

function ink(): { primary: string; secondary: string; grid: string; axis: string } {
  const dark = mode.value === 'dark'
  return {
    primary: dark ? '#F2F4F6' : '#15181D',
    secondary: dark ? '#98A2AE' : '#636D7A',
    grid: dark ? '#262B33' : '#EDF0F3',
    axis: dark ? '#98A2AE' : '#4E5763',
  }
}

const fmt = (v: number) => (props.unit === 'percent' ? formatPercent(v) : formatRate(v))

// uPlot touches matchMedia on import, so load it only when a chart mounts.
async function build() {
  if (!el.value) return
  const { default: UPlot } = await import('uplot')
  if (!el.value) return
  chart?.destroy()
  const c = ink()
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
          stroke: s.tone === 'primary' ? c.primary : c.secondary,
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
watch(mode, () => void build())

onBeforeUnmount(() => {
  observer?.disconnect()
  chart?.destroy()
  chart = null
  hover.value = null
})
</script>

<template>
  <section class="rounded-lg border border-default p-4">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-medium">{{ title }}</h3>
      <ul v-if="series.length > 1" class="flex gap-3 text-xs text-muted">
        <li v-for="s in series" :key="s.label" class="flex items-center gap-1">
          <span
            class="inline-block size-2 rounded-full"
            :class="
              s.tone === 'primary' ? 'bg-ink-950 dark:bg-ink-50' : 'bg-ink-500 dark:bg-ink-400'
            "
          />
          {{ s.label }}
        </li>
      </ul>
    </div>
    <p class="mb-1 flex min-h-5 flex-wrap gap-x-3 text-xs tabular-nums">
      <template v-if="hover">
        <time class="text-muted">{{ hover.time }}</time>
        <span v-for="v in hover.values" :key="v.label" class="text-default">
          <template v-if="series.length > 1">{{ v.label }} </template>{{ v.value }}
        </span>
      </template>
    </p>
    <div ref="el" class="w-full" role="img" :aria-label="`${title} over time`" />
  </section>
</template>
