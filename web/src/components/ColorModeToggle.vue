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
import { useColorMode } from '@vueuse/core'

const mode = useColorMode({ emitAuto: true })
const options = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'auto', label: 'System' },
] as const
</script>

<template>
  <div
    role="radiogroup"
    aria-label="Colour mode"
    class="grid grid-cols-3 gap-1 rounded-[9px] bg-(--nb-soft) p-1 ring-1 ring-default"
  >
    <button
      v-for="o in options"
      :key="o.value"
      type="button"
      role="radio"
      :aria-checked="mode === o.value"
      class="rounded-[6px] px-2 py-1 text-xs font-medium motion-safe:transition-colors focus-visible:outline-2 focus-visible:outline-primary"
      :class="
        mode === o.value
          ? 'bg-elevated text-highlighted shadow-xs ring-1 ring-default'
          : 'text-muted hover:text-highlighted'
      "
      @click="mode = o.value"
    >
      {{ o.label }}
    </button>
  </div>
</template>
