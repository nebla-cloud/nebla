<script setup lang="ts">
import type { Action } from '@/api/types'
import PolledAction from './PolledAction.vue'

defineProps<{ actions: Action[] }>()
</script>

<template>
  <div v-if="actions.length" class="flex flex-col gap-3">
    <PolledAction v-for="a in actions" :key="a.id" :action="a" />
  </div>
</template>
