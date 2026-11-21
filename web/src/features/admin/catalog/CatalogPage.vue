<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import { table, tableWrap, tbody, td, th, thead, tr } from '@/lib/ui'
import CatalogCell from './CatalogCell.vue'
import ImageForm from './ImageForm.vue'
import {
  useAdminImages,
  useCatalog,
  useDeleteImage,
  type AdminImage,
  type CatalogEntry,
  type CatalogLocationState,
} from './queries'

const toast = useToast()
const locations = useAdminLocations()
const catalog = useCatalog()
const images = useAdminImages()
const del = useDeleteImage()

const formOpen = ref(false)
const editing = ref<AdminImage | undefined>()
const removing = ref<AdminImage | null>(null)
const removeError = ref<string | null>(null)
const removeOpen = computed({
  get: () => removing.value !== null,
  set: (v) => {
    if (!v) removing.value = null
  },
})

const columns = computed(() => locations.data.value ?? [])
const rows = computed(() =>
  [...(catalog.data.value ?? [])].sort((a, b) => Number(a.retired) - Number(b.retired)),
)
const ownImages = computed(() => (images.data.value ?? []).filter((i) => !i.catalog_key))
const loadError = computed(() => locations.error.value ?? catalog.error.value)
const pending = computed(() => locations.isPending.value || catalog.isPending.value)

function openForm(i?: AdminImage) {
  editing.value = i
  formOpen.value = true
}

function ask(i: AdminImage) {
  removeError.value = null
  removing.value = i
}

function saved() {
  formOpen.value = false
  toast.add({ title: 'Image saved', color: 'success' })
}

async function remove() {
  if (!removing.value) return
  removeError.value = null
  try {
    await del.mutateAsync(removing.value.id)
    toast.add({
      title: `Deleted ${removing.value.description || removing.value.name}`,
      color: 'success',
    })
    removing.value = null
  } catch (e) {
    removeError.value = errorMessage(e)
  }
}

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
  <div class="flex flex-col gap-4">
    <PageHeader
      title="OS images"
      description="Official cloud images Nebla builds for each location. People only see the ones that are ready."
    />

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
      <div :class="[tableWrap, 'overflow-x-auto']">
        <table :class="[table, 'sm:min-w-max']">
          <thead :class="thead">
            <tr>
              <th scope="col" :class="th">Image</th>
              <th v-for="l in columns" :key="l.id" scope="col" :class="th">
                {{ l.name }}
              </th>
            </tr>
          </thead>
          <tbody :class="tbody">
            <tr v-for="e in rows" :key="e.key" :class="tr">
              <th
                scope="row"
                class="w-full text-start font-normal sm:table-cell sm:w-auto sm:px-4 sm:py-3"
              >
                <span
                  class="block font-semibold"
                  :class="e.retired ? 'text-muted' : 'text-highlighted'"
                  >{{ e.description }}</span
                >
                <span v-if="e.retired" class="block text-xs text-muted"
                  >No longer in the catalog</span
                >
              </th>
              <td v-for="l in columns" :key="l.id" :class="td">
                <span aria-hidden="true" class="me-2 text-muted sm:hidden">{{ l.name }}</span>
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

      <Panel data-own-images title="Your own images">
        <template #header-actions>
          <UButton icon="i-lucide-plus" color="neutral" variant="subtle" @click="openForm()">
            Add image
          </UButton>
        </template>
        <p v-if="!ownImages.length" class="text-muted">
          Register a template you made in Proxmox so people can pick it.
        </p>
        <template v-else>
          <p class="mb-3 text-muted">Images registered by hand from your own templates.</p>
          <ul class="divide-y divide-default">
            <li
              v-for="i in ownImages"
              :key="i.id"
              class="flex flex-col gap-2 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
            >
              <span class="min-w-0">
                <span class="block truncate font-semibold text-highlighted">{{
                  i.description || i.name
                }}</span>
                <span class="block truncate text-xs text-muted">{{ i.name }}</span>
                <span class="block text-sm text-muted">{{
                  locationNames(i.templates.map((t) => t.location_id))
                }}</span>
              </span>
              <span class="flex gap-1">
                <UButton
                  color="neutral"
                  variant="ghost"
                  :aria-label="`Edit ${i.description || i.name}`"
                  @click="openForm(i)"
                >
                  Edit
                </UButton>
                <UButton
                  color="error"
                  variant="ghost"
                  :aria-label="`Delete ${i.description || i.name}`"
                  @click="ask(i)"
                >
                  Delete
                </UButton>
              </span>
            </li>
          </ul>
        </template>
      </Panel>
    </template>

    <UModal
      v-model:open="formOpen"
      :title="editing ? `Edit ${editing.description || editing.name}` : 'Add image'"
      description="Point Nebla at a template you made in Proxmox."
    >
      <template #body>
        <ImageForm
          :key="editing?.id ?? 'new'"
          :item="editing"
          @done="saved"
          @cancel="formOpen = false"
        />
      </template>
    </UModal>

    <ConfirmDialog
      v-model:open="removeOpen"
      title="Delete this image?"
      :body="
        removeError ??
        'People will no longer be able to pick it. The template in Proxmox is left alone.'
      "
      confirm-label="Delete image"
      danger
      :loading="del.isPending.value"
      @confirm="remove"
    />
  </div>
</template>
