<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
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
  <div class="flex flex-col gap-6 p-4 sm:p-8">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex max-w-prose flex-col gap-1">
        <h1 class="text-2xl font-bold tracking-tight text-highlighted">Locations</h1>
        <p class="text-muted">
          A location is a Proxmox cluster Nebla can place servers on. Nebla finds its nodes when you
          connect it.
        </p>
      </div>
      <UButton v-if="!creating && locations?.length" @click="open"> Connect a location </UButton>
    </header>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />

    <section v-if="creating" class="rounded-lg border border-default p-4 sm:p-6">
      <h2 class="mb-4 text-lg font-bold tracking-tight text-highlighted">Connect a location</h2>
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
    </section>

    <ul
      v-if="enrollments?.length"
      class="flex flex-col divide-y divide-default rounded-lg border border-default"
    >
      <li v-for="e in enrollments" :key="e.id">
        <RouterLink
          :to="{ name: 'admin-location-enroll', params: { enrollmentId: e.id } }"
          class="block px-4 py-3 hover:bg-elevated focus-visible:outline-2 focus-visible:outline-primary"
        >
          <span class="font-bold text-highlighted">{{ e.name }}</span>
          <span v-if="e.location_id !== null" class="text-muted"> (re-run)</span>
          <span class="text-muted"> · {{ stateLabel(e) }}</span>
        </RouterLink>
      </li>
    </ul>

    <p v-else-if="isPending" class="text-muted">Loading locations</p>

    <EmptyState
      v-else-if="!locations?.length && !creating"
      title="No locations yet"
      body="Connect a Proxmox cluster so people can create servers on it."
    >
      <UButton @click="open">Connect a location</UButton>
    </EmptyState>

    <ul
      v-else-if="locations"
      class="flex flex-col divide-y divide-default rounded-lg border border-default"
    >
      <li v-for="l in locations" :key="l.id">
        <RouterLink
          :to="{ name: 'admin-location', params: { locationId: l.id } }"
          class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 px-4 py-3 hover:bg-elevated focus-visible:outline-2 focus-visible:outline-primary"
        >
          <span class="font-bold text-highlighted">{{ l.name }}</span>
          <span class="text-sm text-muted">
            {{ plural(l.nodes.length, 'node') }},
            {{ l.nodes.filter((n) => n.enabled).length }} enabled
          </span>
          <span class="w-full truncate text-sm text-muted">{{ l.pve.url }}</span>
        </RouterLink>
      </li>
    </ul>
  </div>
</template>
