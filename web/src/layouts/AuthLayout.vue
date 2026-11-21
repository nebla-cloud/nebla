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
import Lockup from '@/components/Lockup.vue'

defineProps<{ title: string; description?: string }>()
</script>

<template>
  <main
    class="flex overflow-x-clip min-h-dvh flex-col items-center px-4 pt-[12vh] pb-12"
  >
    <Lockup :height="28" class="mb-8" />
    <div
      class="w-full max-w-sm rounded-[var(--nb-radius-panel)] border border-default bg-elevated p-6 sm:p-8"
    >
      <header class="mb-6 flex flex-col gap-1.5">
        <h1 class="nb-h1 !text-[1.5rem] text-highlighted">{{ title }}</h1>
        <p v-if="description" class="text-sm text-muted">{{ description }}</p>
      </header>
      <slot />
    </div>
    <div v-if="$slots.after" class="mt-6 text-sm text-muted">
      <slot name="after" />
    </div>
  </main>
</template>
