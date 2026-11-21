<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: 'md' | 'lg' }>(), { size: 'md' })
</script>

<template>
  <span
    aria-hidden="true"
    class="grid shrink-0 place-items-center rounded-[6px] bg-inverted font-bold text-inverted"
    :class="size === 'lg' ? 'size-[30px] text-[13px]' : 'size-[22px] text-xs'"
    >{{ name.charAt(0).toUpperCase() }}</span
  >
</template>
