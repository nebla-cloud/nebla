<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCollapsible from '@nuxt/ui/components/Collapsible.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import EmptyState from '@/components/EmptyState.vue'
import { typeSpec } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import { useServers } from '@/features/servers/queries'
import AddSshKeyModal from '@/features/sshkeys/AddSshKeyModal.vue'
import { useSshKeys, type SshKey } from '@/features/sshkeys/queries'
import CreateSummary from './CreateSummary.vue'
import {
  MAX_USER_DATA,
  PICK_FAMILY,
  availableImages,
  availableTypes,
  createBlocker,
  defaultName,
  networkChoice,
  noAddresses,
  pickFamilies,
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

const form = reactive<CreateForm>({
  locationId: null,
  imageId: null,
  typeId: null,
  ipv4: true,
  ipv6: false,
  sshKeyIds: [],
  userData: '',
  name: '',
  nameTouched: false,
})
const ipTouched = ref(false)
const submitting = ref(false)
const submitError = ref<string | null>(null)

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
const existing = computed(() => (servers.value ?? []).map((s) => s.name))

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

function onNameInput(v: string) {
  form.nameTouched = v.trim() !== ''
}

const quota = computed(() => {
  const p = project.value
  if (!p?.quota || !p.usage || !serverType.value) return { ok: true, lines: [] }
  return quotaAfter(p.quota, p.usage, serverType.value, Number(form.ipv4) + Number(form.ipv6))
})

const blocker = computed(() =>
  submitting.value
    ? null
    : createBlocker(form, ctx.value, { quotaOk: quota.value.ok, existing: existing.value }),
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
    public_net: { ipv4: form.ipv4, ipv6: form.ipv6 },
    ...(form.userData.trim() ? { user_data: form.userData } : {}),
  } as components['schemas']['ServerInput']
  try {
    const res = unwrap(await api.POST('/servers', { body }))
    qc.setQueryData(['action', res.action.id], res.action)
    qc.setQueryData(['server', res.server.id], res.server)
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

const chip =
  'inline-flex cursor-pointer items-center rounded-md border border-accented px-3 py-1.5 text-sm text-default transition-colors hover:bg-elevated peer-checked:border-inverted peer-checked:font-semibold peer-checked:text-highlighted peer-checked:ring-1 peer-checked:ring-inverted peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary peer-disabled:cursor-not-allowed peer-disabled:border-dashed peer-disabled:text-dimmed peer-disabled:hover:bg-transparent'
const legend = 'mb-2 text-sm font-semibold text-highlighted'
</script>

<template>
  <div class="flex flex-col gap-5 pb-40 lg:pb-0">
    <nav class="text-sm text-muted">
      <RouterLink
        :to="{ name: 'servers', params: { projectId } }"
        class="hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
      >
        Servers
      </RouterLink>
      <span aria-hidden="true"> / </span>
      <span aria-current="page" class="text-default">Create server</span>
    </nav>
    <h1 class="text-xl font-bold tracking-tight text-highlighted">Create server</h1>

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
      <form :id="FORM_ID" class="flex min-w-0 flex-col gap-7" novalidate @submit.prevent="submit">
        <fieldset>
          <legend :class="legend">Location</legend>
          <div class="flex flex-wrap gap-2">
            <label v-for="l in ctx.locations" :key="l.id" class="relative">
              <input
                v-model="form.locationId"
                type="radio"
                name="location"
                :value="l.id"
                class="peer sr-only"
              />
              <span :class="chip">{{ l.name }}</span>
            </label>
          </div>
        </fieldset>

        <fieldset>
          <legend :class="legend">Image</legend>
          <p v-if="!imageOptions.length" class="text-sm text-muted">
            No images yet. Ask your Nebla admin to enable one.
          </p>
          <div v-else class="flex flex-wrap gap-2">
            <label
              v-for="o in imageOptions"
              :key="o.image.id"
              class="relative"
              :title="o.disabledReason ?? undefined"
            >
              <input
                v-model="form.imageId"
                type="radio"
                name="image"
                :value="o.image.id"
                :disabled="!!o.disabledReason"
                class="peer sr-only"
              />
              <span :class="chip">
                {{ o.image.name }}
                <span v-if="o.disabledReason" class="sr-only">, {{ o.disabledReason }}</span>
              </span>
            </label>
          </div>
          <p v-if="imageOptions.some((o) => o.disabledReason)" class="mt-2 text-xs text-muted">
            Dashed images aren't available in {{ location?.name }}.
          </p>
        </fieldset>

        <fieldset>
          <legend :class="legend">Type</legend>
          <p v-if="!typeOptions.length" class="text-sm text-muted">
            No server types yet. Ask your Nebla admin to add one.
          </p>
          <div
            v-else
            class="flex flex-col divide-y divide-default overflow-hidden rounded-lg border border-default"
          >
            <label
              v-for="o in typeOptions"
              :key="o.type.id"
              class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 has-checked:bg-elevated"
              :class="
                o.disabledReason
                  ? 'cursor-not-allowed text-dimmed'
                  : 'cursor-pointer hover:bg-elevated/60'
              "
            >
              <input
                v-model="form.typeId"
                type="radio"
                name="server_type"
                :value="o.type.id"
                :disabled="!!o.disabledReason"
                class="size-4 accent-(--ui-text-highlighted) focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              />
              <span
                class="min-w-24 font-semibold"
                :class="o.disabledReason ? '' : 'text-highlighted'"
              >
                {{ o.type.name }}
              </span>
              <span class="text-sm" :class="o.disabledReason ? '' : 'text-default'">
                {{ typeSpec(o.type) }}
              </span>
              <span v-if="o.disabledReason" class="text-xs sm:ml-auto">{{ o.disabledReason }}</span>
            </label>
          </div>
        </fieldset>

        <fieldset>
          <legend :class="legend">Networking</legend>
          <div class="flex flex-wrap gap-2">
            <label
              v-for="fam in ['ipv4', 'ipv6'] as const"
              :key="fam"
              class="relative"
              :title="nets[fam] ?? undefined"
            >
              <input
                v-model="form[fam]"
                type="checkbox"
                :name="fam"
                :disabled="!!nets[fam]"
                class="peer sr-only"
                @change="ipTouched = true"
              />
              <span :class="chip">{{ fam === 'ipv4' ? 'IPv4' : 'IPv6' }}</span>
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
            <p v-if="!form.ipv4 && !form.ipv6" class="mt-2 text-xs text-muted">{{ PICK_FAMILY }}</p>
          </template>
        </fieldset>

        <fieldset>
          <legend :class="legend">SSH keys</legend>
          <div class="flex flex-wrap items-center gap-2">
            <label v-for="k in sshKeys ?? []" :key="k.id" class="relative">
              <input
                v-model="form.sshKeyIds"
                type="checkbox"
                name="ssh_key"
                :value="k.id"
                class="peer sr-only"
              />
              <span :class="chip">{{ k.name }}</span>
            </label>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-plus"
              @click="keyModal = true"
            >
              Add key
            </UButton>
          </div>
          <p v-if="!sshKeys?.length" class="mt-2 text-xs text-muted">
            No SSH keys in this project yet.
          </p>
          <AddSshKeyModal v-model:open="keyModal" @created="keyCreated" />
        </fieldset>

        <UCollapsible :unmount-on-hide="false">
          <UButton
            color="neutral"
            variant="ghost"
            trailing-icon="i-lucide-chevron-down"
            class="-ml-2.5 font-semibold text-highlighted"
            :ui="{ trailingIcon: 'group-data-[state=open]:rotate-180 transition-transform' }"
          >
            Cloud-init
          </UButton>
          <template #content>
            <div class="flex flex-col gap-1.5 pt-2">
              <label for="user_data" class="text-sm text-muted">
                User data, sent to the server on first boot.
              </label>
              <UTextarea
                id="user_data"
                v-model="form.userData"
                name="user_data"
                :rows="8"
                spellcheck="false"
                placeholder="#cloud-config"
                class="w-full font-mono"
                autoresize
              />
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
      </form>

      <CreateSummary
        v-model:name="form.name"
        :form-id="FORM_ID"
        :location="location"
        :image="image"
        :type="serverType"
        :ipv4="form.ipv4"
        :ipv6="form.ipv6"
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
