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
import type { Action, Server, Volume } from '@/api/types'
import { useLocations } from '@/features/servers-create/queries'
import { useServers } from '@/features/servers/queries'
import { selectFullClass } from '@/lib/ui'
import { createVolume, type VolumeInput } from './queries'
import { MAX_SIZE_GB, MIN_SIZE_GB, serverChoices } from './serverPicker'

// A preset server fixes the location and attaches the new volume to it.
const props = defineProps<{ server?: Server }>()
const emit = defineEmits<{ created: [volume: Volume, action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const NAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const { data: locations } = useLocations()
const { data: servers } = useServers()
const usable = computed(() => (locations.value ?? []).filter((l) => l.volumes_available))

const name = ref('')
const size = ref(String(MIN_SIZE_GB))
const locationId = ref<number | null>(null)
const serverId = ref<number | null>(null)
const nameError = ref<string | null>(null)
const sizeError = ref<string | null>(null)
const formError = ref<string | null>(null)
const pending = ref(false)

const location = computed(() => props.server?.location.id ?? locationId.value)
const choices = computed(() =>
  location.value === null ? [] : serverChoices(servers.value ?? [], location.value),
)

watch(
  usable,
  (l) => {
    if (locationId.value === null || !l.some((x) => x.id === locationId.value))
      locationId.value = l[0]?.id ?? null
  },
  { immediate: true },
)
// Drop a picked server that the new location can't take.
watch(choices, (list) => {
  if (list.find((c) => c.server.id === serverId.value)?.disabledReason !== null)
    serverId.value = null
})
watch(open, (v) => {
  if (v) return
  name.value = ''
  size.value = String(MIN_SIZE_GB)
  serverId.value = null
  nameError.value = sizeError.value = formError.value = null
})

async function submit() {
  nameError.value = sizeError.value = formError.value = null
  const n = name.value.trim()
  const gb = Number(size.value)
  if (!NAME.test(n))
    nameError.value =
      'Use lowercase letters, digits and dashes, starting and ending with a letter or digit.'
  if (
    String(size.value).trim() === '' ||
    !Number.isInteger(gb) ||
    gb < MIN_SIZE_GB ||
    gb > MAX_SIZE_GB
  )
    sizeError.value = `Pick a whole number of GB between ${MIN_SIZE_GB} and ${MAX_SIZE_GB} GB.`
  if (location.value === null) formError.value = 'Pick a location.'
  if (nameError.value || sizeError.value || formError.value) return
  const target = props.server?.id ?? serverId.value
  const body: VolumeInput = { name: n, size: gb, location: String(location.value) }
  if (target !== null) body.server = String(target)
  pending.value = true
  try {
    const res = await createVolume(body)
    emit('created', res.volume, res.action)
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
    title="Create volume"
    description="Extra disk space that keeps its data when you detach it."
  >
    <template #body>
      <form id="create-volume" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <UFormField label="Name" name="name" :error="nameError ?? undefined">
          <UInput
            v-model="name"
            name="name"
            autocomplete="off"
            spellcheck="false"
            placeholder="data"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="Size"
          name="size"
          :help="`${MIN_SIZE_GB} to ${MAX_SIZE_GB} GB. Volumes can grow later but never shrink.`"
          :error="sizeError ?? undefined"
        >
          <UInput
            v-model="size"
            name="size"
            type="number"
            inputmode="numeric"
            :min="MIN_SIZE_GB"
            :max="MAX_SIZE_GB"
            class="w-full sm:w-40"
          >
            <template #trailing><span class="text-sm text-muted">GB</span></template>
          </UInput>
        </UFormField>
        <p v-if="server" class="text-sm text-muted">
          Attaches to <span class="font-semibold text-highlighted">{{ server.name }}</span> in
          {{ server.location.name }}.
        </p>
        <template v-else>
          <UFormField label="Location" name="location">
            <select v-model="locationId" name="location" :class="selectFullClass">
              <option v-for="l in usable" :key="l.id" :value="l.id">{{ l.name }}</option>
            </select>
          </UFormField>
          <UFormField
            label="Attach to"
            name="server"
            hint="Optional"
            help="You can attach it later. It must be in the same location."
          >
            <select v-model="serverId" name="server" :class="selectFullClass">
              <option :value="null">Don't attach</option>
              <option
                v-for="c in choices"
                :key="c.server.id"
                :value="c.server.id"
                :disabled="c.disabledReason !== null"
              >
                {{ c.server.name }}{{ c.disabledReason ? ` (${c.disabledReason})` : '' }}
              </option>
            </select>
          </UFormField>
        </template>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="create-volume" :loading="pending">Create volume</UButton>
      </div>
    </template>
  </UModal>
</template>
