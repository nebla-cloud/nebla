<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { Server } from '@/api/types'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import { useCurrentProject } from '@/lib/project'

defineProps<{ server: Server }>()
const { projectId } = useCurrentProject()
</script>

<template>
  <div class="flex flex-col gap-4">
    <p v-if="!server.firewalls.length" class="text-muted">
      No firewalls apply to this server.
      <RouterLink
        :to="{ name: 'firewalls', params: { projectId } }"
        class="font-semibold text-highlighted hover:underline"
      >
        Manage firewalls
      </RouterLink>
    </p>
    <ListGroup v-else label="Firewalls">
      <ListRow
        v-for="f in server.firewalls"
        :key="`${f.id}-${f.via}`"
        :to="{ name: 'firewall', params: { projectId, firewallId: f.id } }"
      >
        {{ f.name }}
        <template #meta>
          {{ f.via === 'direct' ? 'Applied directly' : `via ${f.via}` }}
        </template>
      </ListRow>
    </ListGroup>
  </div>
</template>
