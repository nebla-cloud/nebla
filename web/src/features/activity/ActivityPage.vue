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
import { ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import { errorMessage } from '@/api/errors'
import ActionList from './ActionList.vue'
import { useActions, type ActionFilter } from './queries'

const status = ref<ActionFilter>('all')
const filters: { value: ActionFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'error', label: 'Failed' },
]
const q = useActions({ status })
const actions = () => q.data.value?.pages.flatMap((p) => p.actions) ?? []
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="Activity" />
    <div class="flex flex-wrap gap-2" role="group" aria-label="Filter by status">
      <UButton
        v-for="f in filters"
        :key="f.value"
        :label="f.label"
        size="sm"
        class="rounded-full"
        :variant="status === f.value ? 'solid' : 'outline'"
        :aria-pressed="status === f.value"
        @click="status = f.value"
      />
    </div>
    <p v-if="q.isPending.value" class="text-muted">Loading activity</p>
    <p v-else-if="q.error.value" role="alert" class="text-error">
      {{ errorMessage(q.error.value) }}
    </p>
    <EmptyState v-else-if="!actions().length" title="No activity" body="Nothing has happened here yet." />
    <template v-else>
      <ActionList :actions="actions()" show-resource />
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
