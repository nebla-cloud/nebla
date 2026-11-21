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
