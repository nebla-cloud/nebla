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
import type { FloatingIp } from '@/api/types'
import { useLocations } from '@/features/servers-create/queries'
import { selectFullClass } from '@/lib/ui'
import { createFloatingIP, type FloatingIPInput } from './queries'

const emit = defineEmits<{ created: [floatingIp: FloatingIp] }>()
const open = defineModel<boolean>('open', { default: false })

const NAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const { data: locations } = useLocations()
const usable = computed(() =>
  (locations.value ?? []).filter((l) => l.networks.ipv4 || l.networks.ipv6),
)

const name = ref('')
const locationId = ref<number | null>(null)
const type = ref<'ipv4' | 'ipv6'>('ipv4')
const nameError = ref<string | null>(null)
const formError = ref<string | null>(null)
const pending = ref(false)

const location = computed(() => usable.value.find((l) => l.id === locationId.value) ?? null)

watch(
  usable,
  (l) => {
    if (locationId.value === null || !l.some((x) => x.id === locationId.value))
      locationId.value = l[0]?.id ?? null
  },
  { immediate: true },
)
// Keep the family on one the location offers.
watch(
  location,
  (l) => {
    if (l && !l.networks[type.value]) type.value = l.networks.ipv4 ? 'ipv4' : 'ipv6'
  },
  { immediate: true },
)
watch(open, (v) => {
  if (v) return
  name.value = ''
  nameError.value = formError.value = null
})

async function submit() {
  nameError.value = formError.value = null
  const n = name.value.trim()
  if (n !== '' && !NAME.test(n))
    nameError.value =
      'Use lowercase letters, digits and dashes, starting and ending with a letter or digit.'
  if (locationId.value === null) formError.value = 'Pick a location.'
  if (nameError.value || formError.value) return
  const body: FloatingIPInput = { type: type.value, location: String(locationId.value) }
  if (n !== '') body.name = n
  pending.value = true
  try {
    const res = await createFloatingIP(body)
    emit('created', res.floating_ip)
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
    title="Create floating IP"
    description="An address you can move between servers in the same location."
  >
    <template #body>
      <form id="create-floating-ip" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <UFormField
          label="Name"
          name="name"
          hint="Optional"
          help="Left empty, it's named after the address."
          :error="nameError ?? undefined"
        >
          <UInput
            v-model="name"
            name="name"
            autocomplete="off"
            spellcheck="false"
            placeholder="web-vip"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Location" name="location">
          <select v-model="locationId" name="location" :class="selectFullClass">
            <option v-for="l in usable" :key="l.id" :value="l.id">{{ l.name }}</option>
          </select>
        </UFormField>
        <UFormField label="Type" name="type">
          <select v-model="type" name="type" :class="selectFullClass">
            <option value="ipv4" :disabled="location !== null && !location.networks.ipv4">
              IPv4
            </option>
            <option value="ipv6" :disabled="location !== null && !location.networks.ipv6">
              IPv6
            </option>
          </select>
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="create-floating-ip" :loading="pending">
          Create floating IP
        </UButton>
      </div>
    </template>
  </UModal>
</template>
