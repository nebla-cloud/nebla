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
withDefaults(defineProps<{ title?: string; as?: 'section' | 'div' }>(), { as: 'section' })
</script>

<template>
  <component
    :is="as"
    class="rounded-(--nb-radius-panel) border border-default bg-elevated p-[1.125rem]"
  >
    <div v-if="title || $slots['header-actions']" class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h2 v-if="title" class="nb-h2 min-w-0 text-highlighted">{{ title }}</h2>
      <div v-if="$slots['header-actions']" class="flex items-center gap-2">
        <slot name="header-actions" />
      </div>
    </div>
    <slot />
  </component>
</template>
