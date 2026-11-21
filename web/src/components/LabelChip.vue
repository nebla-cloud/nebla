<script setup lang="ts">
import { formatLabel } from '../features/servers/labels'

defineProps<{ k: string; v: string }>()
</script>

<template>
  <span class="nb-mono inline-block max-w-40 truncate rounded-full bg-muted px-2 py-0.5 text-xs text-default">
    {{ formatLabel(k, v) }}
  </span>
</template>
