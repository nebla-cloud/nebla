<script setup lang="ts">
import type { Action, Server } from '@/api/types'

defineProps<{ server: Server }>()
defineEmits<{ started: [action: Action] }>()
</script>

<template>
  <p>Coming soon</p>
</template>
