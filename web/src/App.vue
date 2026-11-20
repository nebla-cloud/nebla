<script setup lang="ts">
import { computed } from 'vue'
import { useTitle } from '@vueuse/core'
import UApp from '@nuxt/ui/components/App.vue'
import LoadErrorPage from '@/components/LoadErrorPage.vue'
import { useSession } from '@/lib/session'
import { useLoadFailure } from '@/router/loadError'

const { manifest } = useSession()
const failure = useLoadFailure()
useTitle(computed(() => manifest.value?.branding.product_name || 'Nebla'))
</script>

<template>
  <UApp :toaster="{ position: 'bottom-right' }">
    <LoadErrorPage v-if="failure" :message="failure.message" :retry="failure.retry" />
    <RouterView v-else />
  </UApp>
</template>
