<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

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
