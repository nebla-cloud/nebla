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
import UButton from '@nuxt/ui/components/Button.vue'
import type { Action, Server } from '@/api/types'
import { errorMessage } from '@/api/errors'
import ActionList from '@/features/activity/ActionList.vue'
import { useActions } from '@/features/activity/queries'

const props = defineProps<{ server: Server }>()
defineEmits<{ started: [action: Action] }>()

const q = useActions({ serverId: computed(() => props.server.id) })
const actions = computed(() => q.data.value?.pages.flatMap((p) => p.actions) ?? [])
</script>

<template>
  <div class="flex flex-col gap-4">
    <p v-if="q.isPending.value" class="text-muted">Loading activity</p>
    <p v-else-if="q.error.value" role="alert" class="text-error">
      {{ errorMessage(q.error.value) }}
    </p>
    <p v-else-if="!actions.length" class="text-muted">Nothing has happened here yet.</p>
    <template v-else>
      <ActionList :actions="actions" :show-resource="false" />
      <UButton
        v-if="q.hasNextPage.value"
        label="Load more"
        variant="outline"
        class="self-start"
        :loading="q.isFetchingNextPage.value"
        @click="q.fetchNextPage()"
      />
    </template>
  </div>
</template>
