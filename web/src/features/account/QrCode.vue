<script setup lang="ts">
import { computed } from 'vue'
import { renderSVG } from 'uqr'

const props = defineProps<{ value: string }>()
const svg = computed(() => renderSVG(props.value))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div
    class="size-48 rounded-[6px] bg-white p-2 [&>svg]:size-full"
    role="img"
    aria-label="QR code for your authenticator app"
    v-html="svg"
  />
</template>
