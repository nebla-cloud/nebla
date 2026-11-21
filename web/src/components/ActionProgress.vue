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
import UProgress from '@nuxt/ui/components/Progress.vue'
import type { Action } from '@/api/types'
import { actionLabel } from './actionLabel'

const props = defineProps<{ action: Action }>()

const label = computed(() => actionLabel(props.action.command))
const started = computed(() =>
  new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' }).format(
    new Date(props.action.started),
  ),
)
const failed = computed(() => props.action.status === 'error')
</script>

<template>
  <div class="flex flex-col gap-2.5" role="status" aria-live="polite">
    <div class="flex items-baseline justify-between gap-3 text-sm">
      <span class="font-semibold text-highlighted">{{ label }}</span>
      <span class="text-muted">Started at {{ started }}</span>
    </div>
    <UProgress
      :model-value="action.progress"
      :max="100"
      size="sm"
      :color="failed ? 'error' : 'primary'"
      :aria-label="`${label}, ${action.progress}%`"
    />
    <p v-if="failed && action.error" class="text-sm text-error">{{ action.error.message }}</p>
  </div>
</template>
