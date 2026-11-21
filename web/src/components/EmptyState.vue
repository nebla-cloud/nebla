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
defineProps<{ title: string; body: string }>()
</script>

<template>
  <section
    class="grid items-center gap-8 py-8 lg:gap-10 lg:py-12"
    :class="$slots.aside ? 'lg:grid-cols-2' : ''"
  >
    <div class="flex flex-col items-start gap-4">
      <div class="flex max-w-prose flex-col gap-2">
        <h2 class="text-[1.375rem] font-extrabold tracking-[-0.035em] text-highlighted lg:text-[1.75rem]">
          {{ title }}
        </h2>
        <p class="text-muted">{{ body }}</p>
      </div>
      <div v-if="$slots.default" class="w-full">
        <slot />
      </div>
    </div>
    <div v-if="$slots.aside" class="min-w-0">
      <slot name="aside" />
    </div>
  </section>
</template>
