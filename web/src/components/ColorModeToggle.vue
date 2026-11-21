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
