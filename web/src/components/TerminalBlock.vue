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
withDefaults(defineProps<{ caption?: string; as?: 'div' | 'pre' }>(), { as: 'div' })
</script>

<template>
  <div class="overflow-hidden rounded-[12px] border border-(--ui-border) bg-(--nb-term-bg) text-(--nb-term-fg)">
    <div v-if="caption" class="border-b border-(--nb-term-border) px-4 py-2 text-xs text-(--nb-term-muted)">
      {{ caption }}
    </div>
    <component :is="as" class="nb-mono m-0 px-4 py-3.5 text-[12.5px] leading-[1.7] wrap-anywhere whitespace-pre-wrap">
      <slot />
    </component>
  </div>
</template>
