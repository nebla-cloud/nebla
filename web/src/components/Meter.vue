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

const props = defineProps<{ value: number; max: number; label: string }>()
const pct = computed(() =>
  props.max > 0 ? Math.min(100, Math.max(0, (props.value / props.max) * 100)) : 0,
)
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <div
      role="meter"
      :aria-valuenow="value"
      aria-valuemin="0"
      :aria-valuemax="max"
      :aria-label="label"
      class="h-1.5 overflow-hidden rounded-full bg-muted"
    >
      <div data-fill class="h-full rounded-full" :style="{ width: `${pct}%`, background: 'var(--nb-gradient)' }" />
    </div>
    <span v-if="$slots.default" class="text-sm text-muted tabular-nums"><slot /></span>
  </div>
</template>
