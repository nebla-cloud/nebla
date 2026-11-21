<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import type { LocationEnrollment } from '@/api/types'
import { useAdminLocations } from '@/lib/adminQueries'
import { selectFullClass, table, tableWrap, tbody, td, th, thead, tr } from '@/lib/ui'
import { useFinishEnrollment, type EnrollmentFinish, type EnrollmentFinished } from './queries'

const props = defineProps<{ enrollment: LocationEnrollment }>()
const emit = defineEmits<{ finished: [EnrollmentFinished] }>()

const id = computed(() => props.enrollment.id)
const report = computed(() => props.enrollment.report!)
const rerun = computed(() => props.enrollment.location_id !== null)
const finish = useFinishEnrollment(id)
const { data: locations, isPending: locationsPending } = useAdminLocations()
const current = computed(() => locations.value?.find((l) => l.id === props.enrollment.location_id))

const nodeUrl = (ip: string) => `https://${ip.includes(':') ? `[${ip}]` : ip}:8006`
const own = computed(() => report.value.nodes.find((n) => n.name === report.value.node))
const disks = computed(() => report.value.storages.filter((s) => s.content.includes('images')))
const isos = computed(() =>
  report.value.storages.filter((s) => s.nebla_iso && s.content.includes('iso')),
)
const imports = computed(() =>
  report.value.storages.filter((s) => s.nebla_iso && s.content.includes('import')),
)

const form = reactive({ url: '', disk: '', iso: '', imp: '', vol: '' })
const initialUrl = ref('')
const ready = ref(false)
// Offline members may have no ip; the start URL is already in the input.
const others = computed(() =>
  report.value.nodes.filter((n) => n.ip && nodeUrl(n.ip) !== initialUrl.value),
)

// A Re-run keeps the location's current settings where the report still allows them.
watch(
  [rerun, locationsPending, current],
  () => {
    if (ready.value || (rerun.value && locationsPending.value)) return
    const loc = rerun.value ? current.value : undefined
    const pick = (list: { id: string }[], want: string | undefined, fallback: string) =>
      want && list.some((s) => s.id === want) ? want : fallback
    form.url = loc?.pve.url ?? (own.value?.ip ? nodeUrl(own.value.ip) : '')
    form.disk = pick(disks.value, loc?.disk_storage, disks.value[0]?.id ?? '')
    form.iso = pick(isos.value, loc?.iso_storage, isos.value[0]?.id ?? '')
    form.imp = pick(imports.value, loc?.import_storage, '')
    form.vol = pick(disks.value, loc?.volume_storage, '')
    initialUrl.value = form.url
    ready.value = true
  },
  { immediate: true },
)
const formError = ref<string | null>(null)


async function submit() {
  formError.value = null
  const body: EnrollmentFinish = {
    url: form.url.trim(),
    disk_storage: form.disk,
    iso_storage: form.iso,
  }
  if (form.imp) body.import_storage = form.imp
  if (rerun.value || form.vol) body.volume_storage = form.vol
  try {
    emit('finished', await finish.mutateAsync(body))
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <p v-if="!ready" class="text-muted">Loading location</p>
  <form
    v-else
    class="flex max-w-2xl flex-col gap-5"
    novalidate
    data-testid="enroll-finish"
    @submit.prevent="submit"
  >
    <p class="text-sm text-muted">
      Proxmox {{ report.pve_version }} reported in from {{ report.node }}. Check the details, then
      save.
    </p>
    <UFormField label="Proxmox URL" name="url" help="Where Nebla reaches the Proxmox API.">
      <div class="flex flex-col gap-2">
        <UInput v-model="form.url" name="url" autocomplete="off" class="w-full" />
        <select
          v-if="others.length"
          name="node_url"
          aria-label="Use another node's address"
          :class="selectFullClass"
          @change="
            (e) => (
              (form.url = (e.target as HTMLSelectElement).value),
              ((e.target as HTMLSelectElement).value = '')
            )
          "
        >
          <option value="">Use another node's address</option>
          <option v-for="n in others" :key="n.name" :value="nodeUrl(n.ip)">
            {{ n.name }}: {{ nodeUrl(n.ip) }}
          </option>
        </select>
      </div>
    </UFormField>
    <UFormField label="Disk storage" name="disk_storage" help="Where server disks live.">
      <select v-model="form.disk" name="disk_storage" :class="selectFullClass">
        <option v-for="s in disks" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>
    <UFormField label="ISO storage" name="iso_storage" help="Where Nebla keeps installer images.">
      <select v-model="form.iso" name="iso_storage" :class="selectFullClass">
        <option v-for="s in isos" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>
    <UFormField label="Import storage" name="import_storage" help="Optional. For disk images.">
      <select v-model="form.imp" name="import_storage" :class="selectFullClass">
        <option value="">None</option>
        <option v-for="s in imports" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>
    <UFormField
      label="Volume storage"
      name="volume_storage"
      help="Where project volumes live. Same as server disks uses each node's disk storage."
    >
      <select v-model="form.vol" name="volume_storage" :class="selectFullClass">
        <option value="">Same as server disks</option>
        <option v-for="s in disks" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>

    <div :class="tableWrap">
      <table :class="table">
        <thead :class="thead">
          <tr>
            <th :class="th">Node</th>
            <th :class="th">IP</th>
            <th :class="th">Fingerprint</th>
          </tr>
        </thead>
        <tbody :class="tbody">
          <tr v-for="n in report.nodes" :key="n.name" :class="tr">
            <td :class="[td, 'text-highlighted']">{{ n.name }}</td>
            <td :class="td">{{ n.ip || '—' }}</td>
            <td :class="[td, 'nb-mono text-xs break-all']">{{ n.fingerprint }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <p v-if="!disks.length || !isos.length" class="text-sm text-error">
      The node reported no usable storage for disks or ISOs. Check the node's storage and run setup
      again.
    </p>
    <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
    <div>
      <UButton type="submit" :disabled="!form.disk || !form.iso" :loading="finish.isPending.value">
        {{ rerun ? 'Save' : 'Connect location' }}
      </UButton>
    </div>
  </form>
</template>
