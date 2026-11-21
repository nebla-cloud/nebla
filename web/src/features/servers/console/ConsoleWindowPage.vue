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
import { useRoute } from 'vue-router'
import { useTitle } from '@vueuse/core'
import { useServer } from '../queries'
import ConsolePanel from './ConsolePanel.vue'

function parseServerId(v: unknown): number {
  if (typeof v !== 'string' || !/^[1-9]\d*$/.test(v)) return 0
  const n = Number(v)
  return Number.isSafeInteger(n) ? n : 0
}

const route = useRoute()
const serverId = computed(() => parseServerId(route.params.serverId))
const { data: server } = useServer(serverId)

const name = computed(() => server.value?.name ?? 'Server')
useTitle(computed(() => `${name.value} console`))
</script>

<template>
  <main class="flex h-dvh flex-col gap-2 bg-default p-2">
    <h1 class="truncate px-1 text-sm font-semibold text-highlighted">{{ name }} console</h1>
    <div class="min-h-0 flex-1">
      <ConsolePanel :key="serverId" :server-id="serverId" fill />
    </div>
  </main>
</template>
