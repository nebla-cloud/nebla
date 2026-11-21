<script setup lang="ts">
import { ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import { useAdminLocations } from '@/lib/adminQueries'
import { selectClass as baseSelectClass } from '@/lib/ui'
import { addrFamily, inCidr, rangeSize } from './cidr'
import { useSaveIpPool, type IpPool, type IpPoolInput } from './queries'

const props = defineProps<{ item?: IpPool }>()
const emit = defineEmits<{ done: []; cancel: [] }>()

const save = useSaveIpPool()
const { data: locations } = useAdminLocations()

const locationId = ref(props.item ? String(props.item.location_id) : '')
const family = ref<'ipv4' | 'ipv6'>(props.item?.family ?? 'ipv4')
const bridge = ref(props.item?.bridge ?? '')
const vlan = ref(props.item?.vlan_tag ? String(props.item.vlan_tag) : '')
const cidr = ref(props.item?.cidr ?? '')
const gateway = ref(props.item?.gateway ?? '')
const dns = ref(props.item?.dns.join(', ') ?? '')
const rangeStart = ref(props.item?.range_start ?? '')
const rangeEnd = ref(props.item?.range_end ?? '')
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

const selectClass = `${baseSelectClass} sm:w-full`

async function submit() {
  formError.value = null
  const e: Record<string, string> = {}
  const c = cidr.value.trim()
  const gw = gateway.value.trim()
  const start = rangeStart.value.trim()
  const end = rangeEnd.value.trim()
  const servers = dns.value
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  const tag = vlan.value.trim()

  if (!locationId.value) e.location_id = 'Pick a location.'
  if (!bridge.value.trim()) e.bridge = 'Enter the bridge name.'
  if (tag && !(/^\d+$/.test(tag) && Number(tag) >= 1 && Number(tag) <= 4094)) {
    e.vlan_tag = 'VLAN tag must be between 1 and 4094.'
  }
  const cidrFamily = addrFamily(c.split('/')[0] ?? '')
  if (!c.includes('/') || !cidrFamily || !inCidr(c.split('/')[0]!, c)) {
    e.cidr = 'Enter a valid CIDR, like 203.0.113.0/24.'
  } else if (cidrFamily !== family.value) {
    e.cidr = `This CIDR is ${cidrFamily === 'ipv4' ? 'IPv4' : 'IPv6'}, but the family is ${family.value === 'ipv4' ? 'IPv4' : 'IPv6'}.`
  }
  if (!e.cidr) {
    if (!inCidr(gw, c)) e.gateway = 'Gateway must be inside the CIDR.'
    if (!inCidr(start, c)) e.range_start = 'Range start must be inside the CIDR.'
    if (!inCidr(end, c)) e.range_end = 'Range end must be inside the CIDR.'
    if (!e.range_start && !e.range_end && rangeSize(start, end) === 0n) {
      e.range_end = 'Range end must not come before range start.'
    }
  }
  if (servers.some((s) => addrFamily(s) === null)) e.dns = 'Enter valid DNS server addresses.'
  errors.value = e
  if (Object.keys(e).length) return

  const body: IpPoolInput = {
    location_id: Number(locationId.value),
    family: family.value,
    bridge: bridge.value.trim(),
    cidr: c,
    gateway: gw,
    dns: servers,
    range_start: start,
    range_end: end,
  }
  if (tag) body.vlan_tag = Number(tag)
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
    <div class="grid gap-4 sm:grid-cols-2">
      <UFormField label="Location" name="location_id" :error="errors.location_id">
        <select v-model="locationId" name="location_id" :class="selectClass">
          <option value="" disabled>Choose a location</option>
          <option v-for="l in locations" :key="l.id" :value="String(l.id)">{{ l.name }}</option>
        </select>
      </UFormField>
      <UFormField label="Family" name="family">
        <select v-model="family" name="family" :class="selectClass">
          <option value="ipv4">IPv4</option>
          <option value="ipv6">IPv6</option>
        </select>
      </UFormField>
      <UFormField label="Bridge" name="bridge" :error="errors.bridge">
        <UInput v-model="bridge" name="bridge" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField label="VLAN tag (optional)" name="vlan_tag" :error="errors.vlan_tag">
        <UInput v-model="vlan" name="vlan_tag" inputmode="numeric" class="w-full" />
      </UFormField>
      <UFormField label="CIDR" name="cidr" :error="errors.cidr">
        <UInput v-model="cidr" name="cidr" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField label="Gateway" name="gateway" :error="errors.gateway">
        <UInput v-model="gateway" name="gateway" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField label="Range start" name="range_start" :error="errors.range_start">
        <UInput v-model="rangeStart" name="range_start" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField label="Range end" name="range_end" :error="errors.range_end">
        <UInput v-model="rangeEnd" name="range_end" autocomplete="off" class="w-full" />
      </UFormField>
    </div>
    <UFormField label="DNS servers, comma separated" name="dns" :error="errors.dns">
      <UInput v-model="dns" name="dns" autocomplete="off" class="w-full" />
    </UFormField>
    <div class="flex justify-end gap-2">
      <UButton color="neutral" variant="ghost" @click="emit('cancel')">Cancel</UButton>
      <UButton type="submit" :loading="save.isPending.value">
        {{ item ? 'Save changes' : 'Create IP pool' }}
      </UButton>
    </div>
  </form>
</template>
