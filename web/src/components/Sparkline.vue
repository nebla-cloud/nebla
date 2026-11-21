<script setup lang="ts">
import { computed } from 'vue'

type Tone = 'busy' | 'run' | 'err'
const TONE_VAR: Record<Tone, string> = { busy: '--nb-busy', run: '--nb-run', err: '--nb-err' }

const props = withDefaults(
  defineProps<{
    points: [number, number | null][]
    width: number
    height: number
    tone?: Tone
  }>(),
  { tone: 'busy' },
)

const line = computed(() => {
  const pts = props.points.filter((p): p is [number, number] => p[1] != null)
  if (pts.length < 2) return null
  const t0 = pts[0]![0]
  const span = pts.at(-1)![0] - t0 || 1
  const max = Math.max(...pts.map((p) => p[1]), 0) || 1
  const pad = 1.5
  const h = props.height - pad * 2
  return pts
    .map(([t, v]) => {
      const x = ((t - t0) / span) * props.width
      const y = pad + h - (Math.max(0, v) / max) * h
      return `${+x.toFixed(2)},${+y.toFixed(2)}`
    })
    .join(' ')
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
    <polyline
      v-if="line"
      :points="line"
      fill="none"
      :stroke="`var(${TONE_VAR[tone]})`"
      stroke-width="2"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>
