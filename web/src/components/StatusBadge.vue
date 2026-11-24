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
import StatusArch from './StatusArch.vue'

const props = defineProps<{ status: Server['status']; restoring?: boolean }>()
// A restore has no server status of its own, so it borrows the busy look.
const s = computed(() =>
  props.restoring
    ? { label: 'Restoring', tone: 'busy' as const, pulse: true }
    : serverStatus(props.status),
)
const tone: Record<StatusTone, string> = {
  success: 'border-transparent bg-[var(--nb-run-bg)] text-[var(--nb-run-fg)]',
  busy: 'border-transparent bg-[var(--nb-soft)] text-[var(--nb-busy-fg)]',
  neutral: 'border-(--ui-border) bg-(--nb-soft) text-toned',
  error: 'border-transparent bg-error/10 text-error',
}
</script>

<template>
  <span
    class="inline-flex h-7 items-center gap-[7px] rounded-t-[14px] rounded-b-[6px] border pr-3 pl-2 text-[13px] font-semibold"
    :class="tone[s.tone]"
  >
    <StatusArch :status="restoring ? 'rebuilding' : status" :size="16" decorative />
    {{ s.label }}
  </span>
</template>
