<script setup lang="ts">
import { computed } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import CatalogCell from './CatalogCell.vue'
import { useAdminImages, useCatalog, type CatalogEntry, type CatalogLocationState } from './queries'

const locations = useAdminLocations()
const catalog = useCatalog()
const images = useAdminImages()

const columns = computed(() => locations.data.value ?? [])
const rows = computed(() =>
  [...(catalog.data.value ?? [])].sort((a, b) => Number(a.retired) - Number(b.retired)),
)
const ownImages = computed(() => (images.data.value ?? []).filter((i) => !i.catalog_key))
const loadError = computed(() => locations.error.value ?? catalog.error.value)
const pending = computed(() => locations.isPending.value || catalog.isPending.value)

function cellFor(e: CatalogEntry, locationId: number): CatalogLocationState | undefined {
  return e.locations.find((l) => l.location_id === locationId)
}

function locationNames(ids: number[]): string {
  const names = [...new Set(ids)].map(
    (id) => columns.value.find((l) => l.id === id)?.name ?? `Location ${id}`,
  )
  return names.length ? names.join(', ') : 'No templates'
}
</script>

<template>
  <div class="mx-auto flex w-full max-w-5xl flex-col gap-8 px-4 py-8 sm:px-8">
    <header class="flex flex-col gap-1.5">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">OS images</h1>
      <p class="max-w-prose text-muted">
        Official cloud images Nebla builds for each location. People only see the ones that are
        ready.
      </p>
    </header>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="pending" class="text-muted">Loading OS images…</p>

    <EmptyState
      v-else-if="!columns.length"
      title="Connect a location first"
      body="Nebla builds OS images inside a location. Add one, then come back here to turn images on."
    >
      <UButton :to="{ name: 'admin-locations' }" color="neutral">Add a location</UButton>
    </EmptyState>

    <template v-else>
      <div class="overflow-x-auto rounded-lg border border-default">
        <table class="w-full min-w-max border-collapse text-sm">
          <thead>
            <tr class="border-b border-default">
              <th scope="col" class="px-4 py-2.5 text-start font-medium text-muted">Image</th>
              <th
                v-for="l in columns"
                :key="l.id"
                scope="col"
                class="px-4 py-2.5 text-start font-medium text-muted"
              >
                {{ l.name }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-default">
            <tr v-for="e in rows" :key="e.key">
              <th scope="row" class="px-4 py-3 text-start align-middle font-normal">
                <span
                  class="block font-semibold"
                  :class="e.retired ? 'text-muted' : 'text-highlighted'"
                  >{{ e.description }}</span
                >
                <span v-if="e.retired" class="block text-xs text-muted"
                  >No longer in the catalog</span
                >
              </th>
              <td v-for="l in columns" :key="l.id" class="px-4 py-3 align-middle">
                <CatalogCell
                  v-if="cellFor(e, l.id)"
                  :entry="e"
                  :loc="cellFor(e, l.id)!"
                  :location-name="l.name"
                />
                <span v-else class="text-muted">Not listed</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!rows.length" class="text-muted">The catalog is empty.</p>

      <section v-if="ownImages.length" data-own-images class="flex flex-col gap-3">
        <div class="flex flex-col gap-1">
          <h2 class="text-base font-bold text-highlighted">Your own images</h2>
          <p class="text-muted">Images registered by hand from your own templates.</p>
        </div>
        <ul class="divide-y divide-default overflow-hidden rounded-lg border border-default">
          <li
            v-for="i in ownImages"
            :key="i.id"
            class="flex flex-col gap-0.5 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
          >
            <span class="min-w-0">
              <span class="block truncate font-semibold text-highlighted">{{
                i.description || i.name
              }}</span>
              <span class="block truncate text-xs text-muted">{{ i.name }}</span>
            </span>
            <span class="text-sm text-muted">{{
              locationNames(i.templates.map((t) => t.location_id))
            }}</span>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>
