<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import type { LocationEnrollment } from '@/api/types'
import { useFinishEnrollment, type EnrollmentFinish, type EnrollmentFinished } from './queries'

const props = defineProps<{ enrollment: LocationEnrollment }>()
const emit = defineEmits<{ finished: [EnrollmentFinished] }>()

const id = computed(() => props.enrollment.id)
const report = computed(() => props.enrollment.report!)
const rerun = computed(() => props.enrollment.location_id !== null)
const finish = useFinishEnrollment(id)

const nodeUrl = (ip: string) => `https://${ip}:8006`
const own = computed(() => report.value.nodes.find((n) => n.name === report.value.node))
const others = computed(() => report.value.nodes.filter((n) => n.name !== report.value.node))
const disks = computed(() => report.value.storages.filter((s) => s.content.includes('images')))
const isos = computed(() =>
  report.value.storages.filter((s) => s.nebla_iso && s.content.includes('iso')),
)
const imports = computed(() =>
  report.value.storages.filter((s) => s.nebla_iso && s.content.includes('import')),
)

const form = reactive({
  url: own.value ? nodeUrl(own.value.ip) : '',
  disk: disks.value[0]?.id ?? '',
  iso: isos.value[0]?.id ?? '',
  imp: '',
})
const formError = ref<string | null>(null)

const selectClass =
  'w-full rounded-md border border-accented bg-default px-2.5 py-1.5 text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-primary'

async function submit() {
  formError.value = null
  const body: EnrollmentFinish = {
    url: form.url.trim(),
    disk_storage: form.disk,
    iso_storage: form.iso,
  }
  if (form.imp) body.import_storage = form.imp
  try {
    emit('finished', await finish.mutateAsync(body))
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <form
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
          :class="selectClass"
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
      <select v-model="form.disk" name="disk_storage" :class="selectClass">
        <option v-for="s in disks" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>
    <UFormField label="ISO storage" name="iso_storage" help="Where Nebla keeps installer images.">
      <select v-model="form.iso" name="iso_storage" :class="selectClass">
        <option v-for="s in isos" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>
    <UFormField label="Import storage" name="import_storage" help="Optional. For disk images.">
      <select v-model="form.imp" name="import_storage" :class="selectClass">
        <option value="">None</option>
        <option v-for="s in imports" :key="s.id" :value="s.id">{{ s.id }} ({{ s.type }})</option>
      </select>
    </UFormField>

    <table class="w-full text-left text-sm">
      <thead class="text-muted">
        <tr>
          <th class="py-1 pr-3 font-medium">Node</th>
          <th class="py-1 pr-3 font-medium">IP</th>
          <th class="py-1 font-medium">Fingerprint</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="n in report.nodes" :key="n.name" class="border-t border-default align-top">
          <td class="py-1 pr-3 text-highlighted">{{ n.name }}</td>
          <td class="py-1 pr-3">{{ n.ip }}</td>
          <td class="py-1 font-mono text-xs break-all">{{ n.fingerprint }}</td>
        </tr>
      </tbody>
    </table>

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
