<script setup lang="ts">
defineProps<{ name: string }>()
</script>

<template>
  <span
    aria-hidden="true"
    class="grid size-[22px] shrink-0 place-items-center rounded-md bg-inverted text-[11px] font-bold text-inverted"
    >{{ name.charAt(0).toUpperCase() }}</span
  >
</template>
