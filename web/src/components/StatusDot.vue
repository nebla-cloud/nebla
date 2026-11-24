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
import type { FloatingIp, PrimaryIp, Server, Volume } from '@/api/types'
import {
  floatingIpStatus,
  primaryIpStatus,
  serverStatus,
  volumeStatus,
  type StatusTone,
} from '@/lib/status'

const props = defineProps<{
  status: Server['status'] | Volume['status'] | FloatingIp['status'] | PrimaryIp['status']
  kind?: 'server' | 'volume' | 'floating-ip' | 'primary-ip'
}>()
const s = computed(() =>
  props.kind === 'volume'
    ? volumeStatus(props.status as Volume['status'])
    : props.kind === 'floating-ip'
      ? floatingIpStatus(props.status as FloatingIp['status'])
      : props.kind === 'primary-ip'
        ? primaryIpStatus(props.status as PrimaryIp['status'])
        : serverStatus(props.status as Server['status']),
)
const tone: Record<StatusTone, string> = {
  success: 'bg-[var(--nb-run)]',
  busy: 'bg-[var(--nb-busy)]',
  neutral: 'bg-[var(--nb-off)]',
  error: 'bg-[var(--nb-err)]',
}
</script>

<template>
  <span
    role="img"
    :aria-label="s.label"
    :title="s.label"
    class="inline-block size-2 shrink-0 rounded-full"
    :class="[tone[s.tone], s.pulse && 'motion-safe:animate-pulse']"
  />
</template>
