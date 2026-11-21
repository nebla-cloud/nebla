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
import { computed } from 'vue'
import type { Server } from '@/api/types'
import { serverStatus, type StatusTone } from '@/lib/status'

const props = withDefaults(
  defineProps<{ status: Server['status']; size?: number; decorative?: boolean }>(),
  {
    size: 22,
  },
)
const s = computed(() => serverStatus(props.status))
const stroke: Record<StatusTone, string> = {
  success: 'var(--nb-run)',
  busy: 'var(--nb-busy)',
  neutral: 'var(--nb-off)',
  error: 'var(--nb-err)',
}
</script>

<template>
  <svg
    :role="decorative ? undefined : 'img'"
    :aria-label="decorative ? undefined : s.label"
    :aria-hidden="decorative ? 'true' : undefined"
    viewBox="0 0 64 64"
    :width="size"
    :height="size"
    fill="none"
    class="shrink-0"
  >
    <title v-if="!decorative">{{ s.label }}</title>
    <path
      d="M16 52V31a16 16 0 0 1 32 0v21"
      :stroke="stroke[s.tone]"
      stroke-width="10"
      stroke-linecap="round"
      stroke-linejoin="round"
      :stroke-dasharray="s.tone === 'busy' ? '7 11' : undefined"
      :class="s.tone === 'busy' && 'motion-safe:animate-nb-dash'"
    />
  </svg>
</template>
