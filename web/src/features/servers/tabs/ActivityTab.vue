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
