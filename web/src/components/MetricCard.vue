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
defineProps<{ label: string; value: string | number; unit?: string }>()
</script>

<template>
  <div class="flex flex-col gap-2 rounded-(--nb-radius-panel) border border-default bg-default p-4">
    <span class="text-sm text-muted">{{ label }}</span>
    <span class="text-highlighted">
      <span data-value class="text-2xl font-bold tabular-nums">{{ value }}</span>
      <span v-if="unit" class="ml-1 text-sm text-muted">{{ unit }}</span>
    </span>
    <slot name="chart" />
  </div>
</template>
