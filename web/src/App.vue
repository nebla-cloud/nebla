<script setup lang="ts">
import { computed } from 'vue'
import { useTitle } from '@vueuse/core'
import UApp from '@nuxt/ui/components/App.vue'
import { useSession } from '@/lib/session'

const { manifest } = useSession()
useTitle(computed(() => manifest.value?.branding.product_name || 'Nebla'))
</script>

<template>
  <UApp :toaster="{ position: 'bottom-right' }">
    <RouterView />
  </UApp>
</template>
