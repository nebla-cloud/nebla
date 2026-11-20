<script setup lang="ts">
import { ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
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
    <h1 class="text-2xl font-bold tracking-tight text-highlighted">Activity</h1>
    <div class="flex flex-wrap gap-2" role="group" aria-label="Filter by status">
      <UButton
        v-for="f in filters"
        :key="f.value"
        :label="f.label"
        size="sm"
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
