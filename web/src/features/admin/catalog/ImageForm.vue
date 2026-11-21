<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import { useAdminLocations } from '@/lib/adminQueries'
import { selectFullClass } from '@/lib/ui'
import { useCatalog, useSaveImage, type AdminImage, type ImageInput } from './queries'

interface Row {
  id: number
  location: string
  node: string
  vmid: string
}

const OTHER = 'other'
let nextId = 0

const props = defineProps<{ item?: AdminImage }>()
const emit = defineEmits<{ done: []; cancel: [] }>()

const save = useSaveImage()
const { data: locations } = useAdminLocations()
const catalog = useCatalog()

const flavors = computed(() =>
  [...new Set((catalog.data.value ?? []).map((e) => e.os_flavor))].sort(),
)

const name = ref(props.item?.name ?? '')
const description = ref(props.item?.description ?? '')
const flavor = ref(props.item?.os_flavor ?? '')
const otherPicked = ref(false)
const osVersion = ref(props.item?.os_version ?? '')
const rows = ref<Row[]>(
  props.item?.templates.length
    ? props.item.templates.map((t) => ({
        id: nextId++,
        location: String(t.location_id),
        node: t.node ?? '',
        vmid: String(t.vmid),
      }))
    : [{ id: nextId++, location: '', node: '', vmid: '' }],
)
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

// An existing flavor the catalog doesn't list opens as Other.
watch(
  () => catalog.isPending.value,
  (pending) => {
    if (!pending && props.item && !flavors.value.includes(props.item.os_flavor)) {
      otherPicked.value = true
    }
  },
  { immediate: true },
)
const selectValue = computed({
  get: () => (otherPicked.value ? OTHER : flavor.value),
  set: (v: string) => {
    otherPicked.value = v === OTHER
    flavor.value = v === OTHER ? '' : v
  },
})


function nodesFor(location: string): string[] {
  const l = locations.value?.find((x) => String(x.id) === location)
  return l ? l.nodes.map((n) => n.name) : []
}

function pickLocation(r: Row, v: string) {
  r.location = v
  r.node = ''
}

function addRow() {
  rows.value.push({ id: nextId++, location: '', node: '', vmid: '' })
}

function removeRow(i: number) {
  rows.value.splice(i, 1)
}

async function submit() {
  formError.value = null
  const e: Record<string, string> = {}
  const flavorName = flavor.value.trim()
  if (!name.value.trim()) e.name = 'Give the image a name.'
  if (!flavorName) {
    if (otherPicked.value) e.os_flavor_other = 'Enter the OS flavor.'
    else e.os_flavor = 'Choose the OS flavor.'
  }
  if (!osVersion.value.trim()) e.os_version = 'Enter the OS version.'
  const templates: ImageInput['templates'] = []
  const seen = new Set<string>()
  rows.value.forEach((r) => {
    if (!r.location) e[`location-${r.id}`] = 'Choose a location.'
    const vmid = Number(r.vmid)
    if (r.vmid.trim() === '' || !Number.isInteger(vmid) || vmid <= 0) {
      e[`vmid-${r.id}`] = 'Enter the VMID as a whole number.'
    }
    if (!r.location) return
    const k = `${r.location}/${r.node}`
    if (seen.has(k)) e[`node-${r.id}`] = 'Each location and node can have only one template.'
    seen.add(k)
    const t: ImageInput['templates'][number] = {
      location_id: Number(r.location),
      vmid: Number(r.vmid),
    }
    if (r.node) t.node = r.node
    templates.push(t)
  })
  errors.value = e
  if (Object.keys(e).length) return

  const body: ImageInput = {
    name: name.value.trim(),
    os_flavor: flavorName,
    os_version: osVersion.value.trim(),
    templates,
  }
  if (description.value.trim()) body.description = description.value.trim()
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
    <UFormField label="Display name (optional)" name="description">
      <UInput v-model="description" name="description" autocomplete="off" class="w-full" />
    </UFormField>
    <UFormField label="Name" name="name" :error="errors.name">
      <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
    </UFormField>
    <div class="grid gap-4 sm:grid-cols-2">
      <UFormField label="OS flavor" name="os_flavor" :error="errors.os_flavor">
        <select v-model="selectValue" name="os_flavor" :class="selectFullClass">
          <option value="" disabled>Choose a flavor</option>
          <option v-for="f in flavors" :key="f" :value="f">{{ f }}</option>
          <option :value="OTHER">Other</option>
        </select>
      </UFormField>
      <UFormField label="OS version" name="os_version" :error="errors.os_version">
        <UInput v-model="osVersion" name="os_version" autocomplete="off" class="w-full" />
      </UFormField>
    </div>
    <UFormField
      v-if="otherPicked"
      label="Other flavor"
      name="os_flavor_other"
      :error="errors.os_flavor_other"
    >
      <UInput v-model="flavor" name="os_flavor_other" autocomplete="off" class="w-full" />
    </UFormField>

    <fieldset class="flex flex-col gap-3">
      <legend class="mb-1 text-sm font-medium text-highlighted">Templates</legend>
      <div
        v-for="(r, i) in rows"
        :key="r.id"
        class="flex flex-col gap-3 rounded-[12px] border border-default p-3"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="text-sm font-medium text-highlighted">Template {{ i + 1 }}</span>
          <UButton
            v-if="rows.length > 1"
            color="neutral"
            variant="ghost"
            size="sm"
            icon="i-lucide-trash-2"
            :aria-label="`Remove template ${i + 1}`"
            @click="removeRow(i)"
          />
        </div>
        <div class="grid gap-3 sm:grid-cols-3">
          <UFormField label="Location" :name="`location-${i}`" :error="errors[`location-${r.id}`]">
            <select
              :value="r.location"
              :name="`location-${i}`"
              :class="selectFullClass"
              @change="pickLocation(r, ($event.target as HTMLSelectElement).value)"
            >
              <option value="" disabled>Choose a location</option>
              <option v-for="l in locations" :key="l.id" :value="String(l.id)">{{ l.name }}</option>
            </select>
          </UFormField>
          <UFormField label="Node" :name="`node-${i}`" :error="errors[`node-${r.id}`]">
            <select v-model="r.node" :name="`node-${i}`" :class="selectFullClass">
              <option value="">Shared storage</option>
              <option v-for="n in nodesFor(r.location)" :key="n" :value="n">{{ n }}</option>
            </select>
          </UFormField>
          <UFormField label="VMID" :name="`vmid-${i}`" :error="errors[`vmid-${r.id}`]">
            <UInput
              v-model="r.vmid"
              :name="`vmid-${i}`"
              inputmode="numeric"
              autocomplete="off"
              class="w-full"
            />
          </UFormField>
        </div>
      </div>
      <div>
        <UButton
          data-add-template
          color="neutral"
          variant="subtle"
          size="sm"
          icon="i-lucide-plus"
          @click="addRow"
        >
          Add another template
        </UButton>
      </div>
    </fieldset>

    <div class="flex justify-end gap-2">
      <UButton color="neutral" variant="ghost" @click="emit('cancel')">Cancel</UButton>
      <UButton type="submit" :loading="save.isPending.value">
        {{ item ? 'Save changes' : 'Add image' }}
      </UButton>
    </div>
  </form>
</template>
