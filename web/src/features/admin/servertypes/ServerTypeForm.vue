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
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import type { ServerType } from '@/api/types'
import ChoiceCard from '@/components/ChoiceCard.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import { formatMemory } from '@/lib/format'
import { checkboxClass } from '@/lib/ui'
import { useAdminServerTypes, useSaveServerType, type ServerTypeInput } from './queries'
import { SERVER_TYPE_TEMPLATES, type ServerTypeTemplate } from './templates'

const props = defineProps<{ item?: ServerType }>()
const emit = defineEmits<{ done: []; cancel: [] }>()

const save = useSaveServerType()
const { data: locations } = useAdminLocations()

const name = ref(props.item?.name ?? '')
const description = ref(props.item?.description ?? '')
const cores = ref(props.item ? String(props.item.cores) : '')
const memoryGb = ref(props.item ? String(props.item.memory_mb / 1024) : '')
const diskGb = ref(props.item ? String(props.item.disk_gb) : '')
const scope = ref<'all' | 'some'>(props.item?.locations.length ? 'some' : 'all')
const picked = ref<number[]>([...(props.item?.locations ?? [])])
const deprecated = ref(props.item?.deprecated ?? false)
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

const { data: existingTypes } = useAdminServerTypes()
const takenNames = computed(
  () => new Set((existingTypes.value ?? []).map((t) => t.name.trim().toLowerCase())),
)

function taken(t: ServerTypeTemplate): boolean {
  return takenNames.value.has(t.name.toLowerCase())
}

const selected = computed(() =>
  SERVER_TYPE_TEMPLATES.find(
    (t) =>
      name.value === t.name &&
      cores.value === String(t.cores) &&
      memoryGb.value === String(t.memory_mb / 1024) &&
      diskGb.value === String(t.disk_gb),
  ),
)

const templateKey = computed({
  get: () => selected.value?.key ?? '',
  set: (key: string) => {
    const t = SERVER_TYPE_TEMPLATES.find((x) => x.key === key)
    if (t) applyTemplate(t)
  },
})

function applyTemplate(t: ServerTypeTemplate) {
  name.value = t.name
  description.value = t.description
  cores.value = String(t.cores)
  memoryGb.value = String(t.memory_mb / 1024)
  diskGb.value = String(t.disk_gb)
  errors.value = {}
  formError.value = null
}

const hasLocations = computed(() => (locations.value?.length ?? 0) > 0)

function positive(v: string): number | null {
  const n = Number(v)
  return v.trim() !== '' && Number.isFinite(n) && n > 0 ? n : null
}

async function submit() {
  formError.value = null
  const e: Record<string, string> = {}
  const c = positive(cores.value)
  const m = positive(memoryGb.value)
  const d = positive(diskGb.value)
  if (!name.value.trim()) e.name = 'Give the server type a name.'
  else if (!/^[a-z0-9-]{1,32}$/.test(name.value.trim())) {
    e.name = 'Use 1 to 32 lowercase letters, numbers or hyphens.'
  }
  if (c === null || !Number.isInteger(c)) e.cores = 'Enter a whole number of cores.'
  if (m === null) e.memory_gb = 'Enter the memory in GB.'
  if (d === null || !Number.isInteger(d)) e.disk_gb = 'Enter the disk size as a whole number of GB.'
  if (scope.value === 'some' && picked.value.length === 0) {
    e.locations = 'Pick at least one location, or choose all locations.'
  }
  errors.value = e
  if (Object.keys(e).length) return

  const body: ServerTypeInput = {
    name: name.value.trim(),
    cores: c!,
    memory_mb: Math.round(m! * 1024),
    disk_gb: d!,
    deprecated: deprecated.value,
  }
  if (description.value.trim()) body.description = description.value.trim()
  if (scope.value === 'some') body.locations = [...picked.value]
  try {
    await save.mutateAsync({ id: props.item?.id, body })
    emit('done')
  } catch (err) {
    formError.value = errorMessage(err)
  }
}
</script>

<template>
  <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
    <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
    <template v-if="!item">
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium text-highlighted">Start from a template</legend>
        <div class="grid grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-2.5">
          <ChoiceCard
            v-for="t in SERVER_TYPE_TEMPLATES"
            :key="t.key"
            v-model="templateKey"
            type="radio"
            name="template"
            variant="arch"
            :value="t.key"
            :disabled="taken(t)"
          >
            <span class="text-[1.0625rem]">{{ t.name }}</span>
            <template #meta>
              <span v-if="taken(t)">Already added</span>
              <span v-else class="flex flex-wrap gap-x-2.5">
                <span>{{ t.cores }} vCPU</span>
                <span>{{ formatMemory(t.memory_mb) }}</span>
                <span>{{ t.disk_gb }} GB</span>
              </span>
            </template>
          </ChoiceCard>
        </div>
      </fieldset>
      <div class="flex items-center gap-2 text-xs text-muted" aria-hidden="true">
        <span class="h-px flex-1 bg-(--ui-border)" />
        or fill in yourself
        <span class="h-px flex-1 bg-(--ui-border)" />
      </div>
    </template>
    <UFormField label="Name" name="name" :error="errors.name">
      <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
    </UFormField>
    <UFormField label="Description (optional)" name="description">
      <UInput v-model="description" name="description" autocomplete="off" class="w-full" />
    </UFormField>
    <div class="grid gap-4 sm:grid-cols-3">
      <UFormField label="Cores" name="cores" :error="errors.cores">
        <UInput v-model="cores" name="cores" inputmode="numeric" class="w-full" />
      </UFormField>
      <UFormField label="Memory (GB)" name="memory_gb" :error="errors.memory_gb">
        <UInput v-model="memoryGb" name="memory_gb" inputmode="decimal" class="w-full" />
      </UFormField>
      <UFormField label="Disk (GB)" name="disk_gb" :error="errors.disk_gb">
        <UInput v-model="diskGb" name="disk_gb" inputmode="numeric" class="w-full" />
      </UFormField>
    </div>

    <fieldset class="flex flex-col gap-2">
      <legend class="mb-1 text-sm font-medium text-highlighted">Available in</legend>
      <label class="flex items-center gap-2 text-sm text-default">
        <input v-model="scope" type="radio" name="scope" value="all" :class="checkboxClass" />
        All locations
      </label>
      <label class="flex items-center gap-2 text-sm text-default">
        <input v-model="scope" type="radio" name="scope" value="some" :class="checkboxClass" />
        Only some locations
      </label>
      <div v-if="scope === 'some'" class="ml-6 flex flex-col gap-2">
        <p v-if="!hasLocations" class="text-sm text-muted">No locations yet.</p>
        <label
          v-for="l in locations"
          :key="l.id"
          class="flex items-center gap-2 text-sm text-default"
        >
          <input
            v-model="picked"
            type="checkbox"
            :name="`location-${l.id}`"
            :value="l.id"
            :class="checkboxClass"
          />
          {{ l.name }}
        </label>
        <p v-if="errors.locations" class="text-sm text-error">{{ errors.locations }}</p>
      </div>
    </fieldset>

    <label class="flex items-center gap-2 text-sm text-default">
      <input v-model="deprecated" type="checkbox" name="deprecated" :class="checkboxClass" />
      Deprecated (hide from new servers)
    </label>

    <div class="flex justify-end gap-2">
      <UButton color="neutral" variant="ghost" @click="emit('cancel')">Cancel</UButton>
      <UButton type="submit" :loading="save.isPending.value">
        {{ item ? 'Save changes' : 'Create server type' }}
      </UButton>
    </div>
  </form>
</template>
