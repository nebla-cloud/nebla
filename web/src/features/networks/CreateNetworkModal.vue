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
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Network } from '@/api/types'
import { useLocations } from '@/features/servers-create/queries'
import { selectFullClass } from '@/lib/ui'
import { createNetwork, type NetworkInput } from './queries'

const emit = defineEmits<{ created: [network: Network] }>()
const open = defineModel<boolean>('open', { default: false })

const NAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const { data: locations } = useLocations()
const usable = computed(() => (locations.value ?? []).filter((l) => l.private_networks_available))

const name = ref('')
const locationId = ref<number | null>(null)
const range = ref('')
const nameError = ref<string | null>(null)
const formError = ref<string | null>(null)
const pending = ref(false)

watch(
  usable,
  (l) => {
    if (locationId.value === null || !l.some((x) => x.id === locationId.value))
      locationId.value = l[0]?.id ?? null
  },
  { immediate: true },
)
watch(open, (v) => {
  if (v) return
  name.value = range.value = ''
  nameError.value = formError.value = null
})

async function submit() {
  nameError.value = formError.value = null
  const n = name.value.trim()
  if (!NAME.test(n))
    nameError.value =
      'Use lowercase letters, digits and dashes, starting and ending with a letter or digit.'
  if (locationId.value === null) formError.value = 'Pick a location.'
  if (nameError.value || formError.value) return
  const body: NetworkInput = { name: n, location: String(locationId.value) }
  if (range.value.trim() !== '') body.ip_range = range.value.trim()
  pending.value = true
  try {
    emit('created', await createNetwork(body))
    open.value = false
  } catch (e) {
    formError.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Create network"
    description="A private network connects servers in one location. It isn't reachable from the internet."
  >
    <template #body>
      <form id="create-network" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <p v-if="!usable.length" class="text-muted">
          No location offers private networks yet. Your panel admin can set them up.
        </p>
        <UFormField label="Name" name="name" :error="nameError ?? undefined">
          <UInput
            v-model="name"
            name="name"
            autocomplete="off"
            spellcheck="false"
            placeholder="backend"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Location" name="location">
          <select v-model="locationId" name="location" :class="selectFullClass">
            <option v-for="l in usable" :key="l.id" :value="l.id">{{ l.name }}</option>
          </select>
        </UFormField>
        <UFormField
          label="Range"
          name="ip_range"
          hint="Optional"
          help="An IPv4 range in 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16. Left empty, one is picked for you."
        >
          <UInput
            v-model="range"
            name="ip_range"
            autocomplete="off"
            spellcheck="false"
            placeholder="10.0.0.0/16 (automatic)"
            class="w-full"
          />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="create-network" :loading="pending">Create network</UButton>
      </div>
    </template>
  </UModal>
</template>
