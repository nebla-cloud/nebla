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
defineProps<{ label?: string }>()
// A head slot (column titles) renders above the list, inside the same frame.
const frame = 'overflow-hidden rounded-(--nb-radius-panel) border border-default bg-elevated'
</script>

<template>
  <div v-if="$slots.head" :class="frame">
    <slot name="head" />
    <ul :aria-label="label" class="divide-y divide-default">
      <slot />
    </ul>
  </div>
  <ul v-else :aria-label="label" :class="['divide-y divide-default', frame]">
    <slot />
  </ul>
</template>
