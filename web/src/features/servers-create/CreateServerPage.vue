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
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UCollapsible from '@nuxt/ui/components/Collapsible.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import ChoiceCard from '@/components/ChoiceCard.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import Step from '@/components/Step.vue'
import StepList from '@/components/StepList.vue'
import TerminalBlock from '@/components/TerminalBlock.vue'
import { useCrumbs } from '@/lib/crumbs'
import { formatMemory } from '@/lib/format'
import { selectFullClass } from '@/lib/ui'
import { useCurrentProject } from '@/lib/project'
import OsBadge from '@/features/servers/OsBadge.vue'
import { osParts } from '@/features/servers/osLabel'
import { useServers } from '@/features/servers/queries'
import { DEFAULT_KEEP } from '@/features/backups/queries'
import AddSshKeyModal from '@/features/sshkeys/AddSshKeyModal.vue'
import { usePrimaryIPs } from '@/features/primary-ips/queries'
import { useSshKeys, type SshKey } from '@/features/sshkeys/queries'
import FirewallPicker from '@/features/firewalls/FirewallPicker.vue'
import NetworkPicker from '@/features/networks/NetworkPicker.vue'
import CreateSummary from './CreateSummary.vue'
import {
  MAX_USER_DATA,
  PICK_FAMILY,
  availableImages,
  availableTypes,
  createBlocker,
  defaultName,
  existingIPs,
  newIPCount,
  networkChoice,
  noAddresses,
  pickFamilies,
  publicNet,
  quotaAfter,
  reconcile,
  userDataBytes,
  type CreateContext,
  type CreateForm,
} from './createServer'
import { useImages, useLocations, useServerTypes } from './queries'

const FORM_ID = 'create-server'

const router = useRouter()
const qc = useQueryClient()
const { projectId, project } = useCurrentProject()
const locationsQ = useLocations()
const imagesQ = useImages()
const typesQ = useServerTypes()
const { data: sshKeys } = useSshKeys()
const { data: servers } = useServers()
const { data: primaryIps } = usePrimaryIPs()

useCrumbs(() => [
  { label: 'Servers', to: { name: 'servers', params: { projectId: projectId.value } } },
  { label: 'Create server' },
])

const form = reactive<CreateForm>({
  locationId: null,
  imageId: null,
  typeId: null,
  ipv4: true,
  ipv6: false,
  ipv4Existing: null,
  ipv6Existing: null,
  sshKeyIds: [],
  userData: '',
  name: '',
  nameTouched: false,
})
const ipTouched = ref(false)
const backups = ref(false)
const firewallIds = ref<number[]>([])
const networkIds = ref<number[]>([])
const submitting = ref(false)
const submitError = ref<string | null>(null)

// ChoiceCard values are strings; the form keeps numeric ids.
function idValue(key: 'locationId' | 'imageId' | 'typeId') {
  return computed({
    get: () => (form[key] === null ? undefined : String(form[key])),
    set: (v: unknown) => (form[key] = v == null ? null : Number(v)),
  })
}
const locationValue = idValue('locationId')
const imageValue = idValue('imageId')
const typeValue = idValue('typeId')
const keyValues = computed({
  get: () => form.sshKeyIds.map(String),
  set: (v: unknown) => (form.sshKeyIds = (v as string[]).map(Number)),
})

const ctx = computed<CreateContext>(() => ({
  locations: locationsQ.data.value ?? [],
  images: imagesQ.data.value ?? [],
  types: typesQ.data.value ?? [],
}))
const loading = computed(
  () => locationsQ.isPending.value || imagesQ.isPending.value || typesQ.isPending.value,
)
const loadError = computed(
  () => locationsQ.error.value ?? imagesQ.error.value ?? typesQ.error.value ?? null,
)

const location = computed(() => ctx.value.locations.find((l) => l.id === form.locationId))
const image = computed(() => ctx.value.images.find((i) => i.id === form.imageId))
const serverType = computed(() => ctx.value.types.find((t) => t.id === form.typeId))
const imageOptions = computed(() =>
  location.value ? availableImages(ctx.value.images, location.value.id, location.value.name) : [],
)
const typeOptions = computed(() =>
  location.value ? availableTypes(ctx.value.types, location.value.id, location.value.name) : [],
)
const nets = computed(() =>
  location.value ? networkChoice(location.value) : { ipv4: null, ipv6: null },
)
const backupsOn = computed(() => backups.value && location.value?.backups_available === true)
const backupSlots = computed(() => {
  const max = project.value?.quota?.max_backups ?? 0
  return max > 0
    ? `Uses ${DEFAULT_KEEP} of your ${max} backup slots`
    : `Uses ${DEFAULT_KEEP} backup slots`
})
const backupsOk = computed(() => {
  const p = project.value
  if (!backupsOn.value || !p?.quota || !p.usage) return true
  const max = p.quota.max_backups
  return max <= 0 || p.usage.backups_reserved + DEFAULT_KEEP <= max
})
const existing = computed(() => (servers.value ?? []).map((s) => s.name))
const reusable = computed(() => ({
  ipv4: location.value ? existingIPs(primaryIps.value ?? [], location.value.id, 'ipv4') : [],
  ipv6: location.value ? existingIPs(primaryIps.value ?? [], location.value.id, 'ipv6') : [],
}))
const existingValue = (fam: 'ipv4' | 'ipv6') =>
  computed({
    get: () => (form[`${fam}Existing`] === null ? '' : String(form[`${fam}Existing`])),
    set: (v: string) => (form[`${fam}Existing`] = v === '' ? null : Number(v)),
  })
const existingValues = { ipv4: existingValue('ipv4'), ipv6: existingValue('ipv6') }
const pickedName = (fam: 'ipv4' | 'ipv6') =>
  reusable.value[fam].find((p) => p.id === form[`${fam}Existing`])?.name ?? null

function applyReconcile() {
  Object.assign(form, reconcile({ ...form }, ctx.value))
}

// Picks sensible defaults once the catalog has loaded.
let seeded = false
watch(
  [ctx, loading],
  () => {
    if (loading.value) return
    if (!seeded) {
      seeded = true
      const loc = ctx.value.locations[0]
      if (!loc) return
      form.locationId = loc.id
      Object.assign(form, pickFamilies({ ...form }, loc))
      form.imageId =
        availableImages(ctx.value.images, loc.id).find((o) => !o.disabledReason)?.image.id ?? null
      form.typeId =
        availableTypes(ctx.value.types, loc.id).find((o) => !o.disabledReason)?.type.id ?? null
    }
    applyReconcile()
  },
  { immediate: true },
)

watch(
  () => form.locationId,
  (id, old) => {
    if (old == null || id === old) return
    if (!ipTouched.value) form.ipv4 = true
    const loc = ctx.value.locations.find((l) => l.id === id)
    if (loc) Object.assign(form, pickFamilies({ ...form }, loc))
    submitError.value = null
    networkIds.value = []
    applyReconcile()
  },
)

watch(
  [image, serverType, existing],
  () => {
    if (form.nameTouched || !image.value || !serverType.value) return
    form.name = defaultName(image.value, serverType.value, existing.value)
  },
  { immediate: true },
)

watch(reusable, (r) => {
  for (const fam of ['ipv4', 'ipv6'] as const) {
    const id = form[`${fam}Existing`]
    if (id !== null && !r[fam].some((p) => p.id === id)) form[`${fam}Existing`] = null
  }
})

function onNameInput(v: string) {
  form.nameTouched = v.trim() !== ''
}

const quota = computed(() => {
  const p = project.value
  if (!p?.quota || !p.usage || !serverType.value) return { ok: true, lines: [] }
  return quotaAfter(p.quota, p.usage, serverType.value, newIPCount(form))
})

const blocker = computed(() =>
  submitting.value
    ? null
    : createBlocker(form, ctx.value, {
        quotaOk: quota.value.ok,
        existing: existing.value,
        backupsOk: backupsOk.value,
      }),
)

const userDataSize = computed(() => userDataBytes(form.userData))
const userDataLabel = computed(() => {
  const kib = userDataSize.value / 1024
  return `${kib < 10 ? kib.toFixed(1) : Math.round(kib)} KiB of ${MAX_USER_DATA / 1024} KiB`
})

const keyModal = ref(false)
function keyCreated(k: SshKey) {
  qc.setQueryData<SshKey[]>(['ssh-keys', projectId.value], (old) =>
    old?.some((x) => x.id === k.id) ? old : [...(old ?? []), k],
  )
  if (!form.sshKeyIds.includes(k.id)) form.sshKeyIds.push(k.id)
}

async function submit() {
  if (submitting.value || blocker.value) return
  submitting.value = true
  submitError.value = null
  const body = {
    name: form.name.trim(),
    location: String(form.locationId),
    image: String(form.imageId),
    server_type: String(form.typeId),
    ssh_keys: form.sshKeyIds.map(String),
    public_net: publicNet(form),
    ...(form.userData.trim() ? { user_data: form.userData } : {}),
    ...(backupsOn.value ? { backups: { enabled: true, keep: DEFAULT_KEEP } } : {}),
    ...(firewallIds.value.length
      ? { firewalls: firewallIds.value.map((firewall) => ({ firewall })) }
      : {}),
    ...(networkIds.value.length
      ? { networks: networkIds.value.map((network) => ({ network: String(network) })) }
      : {}),
  } as components['schemas']['ServerInput']
  try {
    const res = unwrap(await api.POST('/servers', { body }))
    qc.setQueryData(['action', res.action.id], res.action)
    qc.setQueryData(['server', projectId.value, res.server.id], res.server)
    void qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
    void qc.invalidateQueries({ queryKey: ['project', projectId.value] })
    await router.push({
      name: 'server',
      params: { projectId: projectId.value, serverId: res.server.id },
      state: { actionId: res.action.id },
    })
  } catch (e) {
    submitError.value = errorMessage(e)
  } finally {
    submitting.value = false
  }
}

const cards = 'flex flex-wrap gap-2.5'
</script>

<template>
  <div class="flex flex-col gap-6 pb-40 lg:pb-0">
    <PageHeader title="Create server" />

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="loading" class="text-muted">Loading…</p>
    <EmptyState
      v-else-if="!ctx.locations.length"
      title="Nowhere to create a server yet"
      body="No locations yet. Ask your Nebla admin to connect one."
    />

    <div
      v-else
      class="grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_18rem] xl:grid-cols-[minmax(0,1fr)_20rem]"
    >
      <form :id="FORM_ID" class="min-w-0" novalidate @submit.prevent="submit">
        <StepList>
          <Step title="Location" :done="form.locationId !== null">
            <fieldset>
              <legend class="sr-only">Location</legend>
              <div :class="cards">
                <ChoiceCard
                  v-for="l in ctx.locations"
                  :key="l.id"
                  v-model="locationValue"
                  type="radio"
                  name="location"
                  :value="String(l.id)"
                  class="min-w-36"
                >
                  {{ l.name }}
                </ChoiceCard>
              </div>
            </fieldset>
            <div v-if="location?.backups_available" class="mt-3">
              <UCheckbox
                v-model="backups"
                name="backups"
                :label="`Enable daily backups (keep ${DEFAULT_KEEP})`"
                :description="backupSlots"
              />
            </div>
          </Step>

          <Step title="Image" :done="form.imageId !== null">
            <fieldset>
              <legend class="sr-only">Image</legend>
              <p v-if="!imageOptions.length" class="text-sm text-muted">
                No images yet. Ask your Nebla admin to enable one.
              </p>
              <div v-else :class="cards">
                <ChoiceCard
                  v-for="o in imageOptions"
                  :key="o.image.id"
                  v-model="imageValue"
                  type="radio"
                  name="image"
                  :value="String(o.image.id)"
                  :disabled="!!o.disabledReason"
                  :title="o.disabledReason ?? o.image.name"
                  class="min-w-40"
                >
                  <span class="flex items-center gap-2.5">
                    <OsBadge :name="o.image.os_flavor" size="lg" />
                    <span class="flex min-w-0 flex-col">
                      <span>{{ osParts(o.image)?.name ?? o.image.name }}</span
                      >{{ ' ' }}
                      <span
                        v-if="o.disabledReason || osParts(o.image)?.version"
                        class="text-[12.5px] font-normal text-muted"
                        >{{ o.disabledReason ?? osParts(o.image)?.version }}</span
                      >
                    </span>
                  </span>
                </ChoiceCard>
              </div>
              <p v-if="imageOptions.some((o) => o.disabledReason)" class="mt-2 text-xs text-muted">
                Dashed images aren't available in {{ location?.name }}.
              </p>
            </fieldset>
          </Step>

          <Step title="Type" :done="form.typeId !== null">
            <fieldset>
              <legend class="sr-only">Type</legend>
              <p v-if="!typeOptions.length" class="text-sm text-muted">
                No server types yet. Ask your Nebla admin to add one.
              </p>
              <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(10rem,1fr))] gap-2.5">
                <ChoiceCard
                  v-for="o in typeOptions"
                  :key="o.type.id"
                  v-model="typeValue"
                  type="radio"
                  name="server_type"
                  variant="arch"
                  :value="String(o.type.id)"
                  :disabled="!!o.disabledReason"
                >
                  <span class="text-[1.0625rem]">{{ o.type.name }}</span>
                  <template #meta>
                    <span v-if="o.disabledReason">{{ o.disabledReason }}</span>
                    <span v-else class="flex flex-wrap gap-x-2.5">
                      <span>{{ o.type.cores }} vCPU</span>
                      <span>{{ formatMemory(o.type.memory_mb) }}</span>
                      <span>{{ o.type.disk_gb }} GB</span>
                    </span>
                  </template>
                </ChoiceCard>
              </div>
            </fieldset>
          </Step>

          <Step title="Networking" :done="form.ipv4 || form.ipv6">
            <fieldset>
              <legend class="sr-only">Networking</legend>
              <div :class="cards">
                <ChoiceCard
                  v-for="fam in ['ipv4', 'ipv6'] as const"
                  :key="fam"
                  v-model="form[fam]"
                  type="checkbox"
                  :name="fam"
                  :value="fam"
                  :disabled="!!nets[fam] && !reusable[fam].length"
                  :title="nets[fam] ?? undefined"
                  class="min-w-36"
                  @update:model-value="ipTouched = true"
                >
                  {{ fam === 'ipv4' ? 'IPv4' : 'IPv6' }}
                </ChoiceCard>
              </div>
              <div v-for="fam in ['ipv4', 'ipv6'] as const" :key="`${fam}-source`">
                <label
                  v-if="form[fam] && reusable[fam].length"
                  class="mt-3 flex max-w-sm flex-col gap-1 text-sm"
                >
                  <span class="text-muted">{{ fam === 'ipv4' ? 'IPv4' : 'IPv6' }} address</span>
                  <select
                    v-model="existingValues[fam].value"
                    :name="`${fam}_source`"
                    :class="selectFullClass"
                  >
                    <option value="">New address</option>
                    <option v-for="p in reusable[fam]" :key="p.id" :value="String(p.id)">
                      Use an existing IP: {{ p.name }} ({{ p.ip }})
                    </option>
                  </select>
                </label>
              </div>
              <p v-if="nets.ipv4 && nets.ipv6" class="mt-2 text-xs text-muted">
                {{ noAddresses(location?.name ?? '') }}
              </p>
              <template v-else>
                <ul v-if="nets.ipv4 || nets.ipv6" class="mt-2 text-xs text-muted">
                  <li v-if="nets.ipv4">{{ nets.ipv4 }}</li>
                  <li v-if="nets.ipv6">{{ nets.ipv6 }}</li>
                </ul>
                <p v-if="!form.ipv4 && !form.ipv6" class="mt-2 text-xs text-muted">
                  {{ PICK_FAMILY }}
                </p>
              </template>
            </fieldset>
          </Step>

          <Step title="SSH keys" :done="form.sshKeyIds.length > 0">
            <fieldset>
              <legend class="sr-only">SSH keys</legend>
              <div :class="cards">
                <ChoiceCard
                  v-for="k in sshKeys ?? []"
                  :key="k.id"
                  v-model="keyValues"
                  type="checkbox"
                  name="ssh_key"
                  :value="String(k.id)"
                  class="min-w-36"
                >
                  {{ k.name }}
                </ChoiceCard>
                <button
                  type="button"
                  class="flex min-w-36 cursor-pointer items-center gap-1.5 rounded-[12px] border border-dashed border-accented bg-default px-3.5 py-3 font-semibold text-default hover:bg-elevated focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--nb-focus)"
                  @click="keyModal = true"
                >
                  <UIcon name="i-lucide-plus" class="size-4" />
                  Add key
                </button>
              </div>
              <p v-if="!sshKeys?.length" class="mt-2 text-xs text-muted">
                No SSH keys in this project yet.
              </p>
              <AddSshKeyModal v-model:open="keyModal" @created="keyCreated" />
            </fieldset>
          </Step>

          <!-- New sections: add a <Step> here; numbers follow position. -->

          <FirewallPicker v-model="firewallIds" />

          <NetworkPicker v-model="networkIds" :location="location" />

          <Step title="Cloud-init" :done="form.userData.trim() !== ''">
            <UCollapsible :unmount-on-hide="false">
              <UButton
                color="neutral"
                variant="ghost"
                trailing-icon="i-lucide-chevron-down"
                class="-ml-2.5 text-muted"
                :ui="{ trailingIcon: 'group-data-[state=open]:rotate-180 transition-transform' }"
              >
                Add user data (optional)
              </UButton>
              <template #content>
                <div class="flex max-w-xl flex-col gap-1.5 pt-2">
                  <label for="user_data" class="text-sm text-muted">
                    User data, sent to the server on first boot.
                  </label>
                  <TerminalBlock
                    caption="user-data"
                    class="has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-(--nb-focus)"
                  >
                    <textarea
                      id="user_data"
                      v-model="form.userData"
                      name="user_data"
                      rows="8"
                      spellcheck="false"
                      placeholder="#cloud-config"
                      class="block w-full resize-y bg-transparent outline-none placeholder:text-(--nb-term-muted)"
                    />
                  </TerminalBlock>
                  <p
                    class="text-xs"
                    :class="userDataSize > MAX_USER_DATA ? 'text-error' : 'text-muted'"
                    aria-live="polite"
                  >
                    {{ userDataLabel }}
                  </p>
                </div>
              </template>
            </UCollapsible>
          </Step>
        </StepList>
      </form>

      <CreateSummary
        v-model:name="form.name"
        :form-id="FORM_ID"
        :location="location"
        :image="image"
        :type="serverType"
        :ipv4="form.ipv4"
        :ipv6="form.ipv6"
        :ipv4-name="pickedName('ipv4')"
        :ipv6-name="pickedName('ipv6')"
        :key-count="form.sshKeyIds.length"
        :quota="quota.lines"
        :blocker="blocker"
        :error="submitError"
        :submitting="submitting"
        @name-input="onNameInput"
      />
    </div>
  </div>
</template>
