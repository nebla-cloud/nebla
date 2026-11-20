<script setup lang="ts">
import { computed } from 'vue'
import BrandMark from '@/components/BrandMark.vue'
import { useSession } from '@/lib/session'

defineProps<{ title: string; description?: string }>()
const { manifest } = useSession()
const product = computed(() => manifest.value?.branding.product_name || 'Nebla')
</script>

<template>
  <main
    class="aurora-glow flex overflow-x-clip min-h-dvh flex-col items-center px-4 pt-[12vh] pb-12"
  >
    <div class="mb-8 flex items-center gap-2.5">
      <BrandMark :size="28" />
      <span class="text-xl font-extrabold tracking-tight text-highlighted">{{ product }}</span>
    </div>
    <div class="w-full max-w-sm rounded-xl border border-default bg-default p-6 shadow-sm sm:p-8">
      <header class="mb-6 flex flex-col gap-1.5">
        <h1 class="text-xl font-bold tracking-tight text-highlighted">{{ title }}</h1>
        <p v-if="description" class="text-sm text-muted">{{ description }}</p>
      </header>
      <slot />
    </div>
    <div v-if="$slots.after" class="mt-6 text-sm text-muted">
      <slot name="after" />
    </div>
  </main>
</template>
