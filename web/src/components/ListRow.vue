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
import { RouterLink, type RouteLocationRaw } from 'vue-router'

defineProps<{ to?: RouteLocationRaw }>()

const inner = 'flex items-center gap-3 px-4 py-3'
const linkClass =
  'hover:bg-(--nb-soft) focus-visible:outline-2 focus-visible:[--nb-focus-offset:-2px] focus-visible:outline-primary'
</script>

<template>
  <li>
    <component
      :is="to ? RouterLink : 'div'"
      v-bind="to ? { to } : {}"
      :class="[inner, to ? linkClass : '']"
    >
      <span v-if="$slots.leading" class="shrink-0"><slot name="leading" /></span>
      <span data-title class="min-w-0 flex-1 truncate font-semibold text-highlighted"><slot /></span>
      <span v-if="$slots.meta" class="shrink-0 text-sm text-muted"><slot name="meta" /></span>
      <span v-if="$slots.trailing" class="shrink-0"><slot name="trailing" /></span>
    </component>
  </li>
</template>
