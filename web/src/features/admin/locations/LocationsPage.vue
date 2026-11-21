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
import { useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import EnrollStartForm from './EnrollStartForm.vue'
import LocationForm from './LocationForm.vue'
import { useEnrollments } from './queries'

const router = useRouter()
const { data: locations, isPending, error } = useAdminLocations()
const { data: enrollments } = useEnrollments()
const creating = ref(false)
const manual = ref(false)

const open = () => {
  manual.value = false
  creating.value = true
}

function stateLabel(e: { state: string; expired: boolean }) {
  if (e.expired) return 'Expired'
  return e.state === 'reported' ? 'Ready to finish' : 'Waiting for setup'
}

function plural(n: number, word: string) {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader
      title="Locations"
      description="A location is a Proxmox cluster Nebla can place servers on. Nebla finds its nodes when you connect it."
    >
      <template v-if="!creating && locations?.length" #actions>
        <UButton icon="i-lucide-plus" @click="open">Connect a location</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />

    <ListGroup v-if="!creating && enrollments?.length" label="Setups in progress">
      <ListRow
        v-for="e in enrollments"
        :key="e.id"
        :to="{ name: 'admin-location-enroll', params: { enrollmentId: e.id } }"
      >
        <span>{{ e.name }}</span>
        <span v-if="e.location_id !== null" class="font-normal text-muted"> (re-run)</span>
        <span class="font-normal text-muted"> · {{ stateLabel(e) }}</span>
      </ListRow>
    </ListGroup>

    <Panel v-if="creating" title="Connect a location">
      <LocationForm
        v-if="manual"
        @cancel="creating = false"
        @saved="(l) => router.push({ name: 'admin-location', params: { locationId: l.id } })"
      />
      <EnrollStartForm
        v-else
        @cancel="creating = false"
        @manual="manual = true"
        @started="
          (id) => router.push({ name: 'admin-location-enroll', params: { enrollmentId: id } })
        "
      />
    </Panel>

    <p v-else-if="isPending" class="text-muted">Loading locations</p>

    <EmptyState
      v-else-if="!locations?.length && !creating"
      title="No locations yet"
      body="Connect a Proxmox cluster so people can create servers on it."
    >
      <UButton icon="i-lucide-plus" @click="open">Connect a location</UButton>
    </EmptyState>

    <ListGroup v-else-if="locations">
      <ListRow
        v-for="l in locations"
        :key="l.id"
        :to="{ name: 'admin-location', params: { locationId: l.id } }"
      >
        <span class="flex min-w-0 flex-col gap-0.5">
          <span class="truncate">{{ l.name }}</span>
          <span class="truncate text-xs font-normal text-muted">{{ l.pve.url }}</span>
        </span>
        <template #meta>
          {{ plural(l.nodes.length, 'node') }},
          {{ l.nodes.filter((n) => n.enabled).length }} enabled
        </template>
      </ListRow>
    </ListGroup>
  </div>
</template>
