<script setup lang="ts">
import { computed } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import Meter from '@/components/Meter.vue'
import Panel from '@/components/Panel.vue'
import type { Location, ServerType } from '@/api/types'
import { formatMemory, typeSpec } from '@/lib/format'
import { osLabel } from '@/features/servers/osLabel'
import type { Image, QuotaLine } from './createServer'

const props = defineProps<{
  formId: string
  location?: Location
  image?: Image
  type?: ServerType
  ipv4: boolean
  ipv6: boolean
  keyCount: number
  quota: QuotaLine[]
  blocker: string | null
  error: string | null
  submitting: boolean
}>()
const name = defineModel<string>('name', { required: true })
const emit = defineEmits<{ nameInput: [value: string] }>()

const network = computed(() => {
  if (props.ipv4 && props.ipv6) return 'IPv4 and IPv6'
  if (props.ipv4) return 'IPv4'
  if (props.ipv6) return 'IPv6'
  return 'No public address'
})

const rows = computed(() => [
  { label: 'Location', value: props.location?.name ?? 'Not picked' },
  { label: 'Image', value: props.image ? osLabel(props.image) : 'Not picked' },
  {
    label: 'Type',
    value: props.type ? `${props.type.name}: ${typeSpec(props.type)}` : 'Not picked',
  },
  { label: 'Networking', value: network.value },
  {
    label: 'SSH keys',
    value: props.keyCount ? `${props.keyCount} ${props.keyCount === 1 ? 'key' : 'keys'}` : 'None',
  },
])

const oneLine = computed(() => {
  if (!props.type || !props.location) return 'Pick an image and a type'
  return `${props.type.name} in ${props.location.name}`
})

function amount(l: QuotaLine, v: number): string {
  if (l.label === 'Memory') return formatMemory(v)
  if (l.label === 'Disk') return `${v} GB`
  return String(v)
}

function quotaText(l: QuotaLine): string {
  if (l.max <= 0) return `${l.label}: ${amount(l, l.used)}, no limit`
  return `${l.label}: ${amount(l, l.used)} of ${amount(l, l.max)}, ${amount(l, l.after)} after this`
}

const over = (l: QuotaLine) => l.max > 0 && l.after > l.used && l.after > l.max
</script>

<template>
  <aside
    aria-label="Summary"
    class="fixed inset-x-0 bottom-0 z-10 flex flex-col gap-2 border-t border-default bg-default px-4 py-3 shadow-lg lg:sticky lg:inset-auto lg:top-10 lg:block lg:border-0 lg:bg-transparent lg:p-0 lg:shadow-none"
  >
    <Panel as="div" class="flex flex-col gap-2 max-lg:contents lg:gap-4">
      <h2 class="nb-h2 hidden text-highlighted lg:block">Summary</h2>
      <dl class="hidden grid-cols-[5.5rem_1fr] gap-x-2 gap-y-1.5 text-sm lg:grid">
        <template v-for="r in rows" :key="r.label">
          <dt class="text-muted">{{ r.label }}</dt>
          <dd class="font-medium break-words text-default">{{ r.value }}</dd>
        </template>
      </dl>

      <div v-if="quota.length" class="hidden flex-col gap-2 text-sm lg:flex">
        <h3 class="font-medium text-muted">Project quota</h3>
        <ul class="flex flex-col gap-2.5">
          <li v-for="l in quota" :key="l.label">
            <Meter v-if="l.max > 0" :value="l.after" :max="l.max" :label="l.label">
              <span :class="over(l) ? 'text-error' : ''">{{ quotaText(l) }}</span>
            </Meter>
            <span v-else class="text-muted">{{ quotaText(l) }}</span>
          </li>
        </ul>
      </div>

      <p class="truncate text-sm text-muted lg:hidden">{{ oneLine }}</p>
      <div class="flex items-end gap-2 lg:flex-col lg:items-stretch">
        <UFormField label="Name" name="name" class="min-w-0 flex-1">
          <UInput
            v-model="name"
            name="name"
            :form="formId"
            autocomplete="off"
            spellcheck="false"
            class="w-full"
            @update:model-value="emit('nameInput', String($event))"
          />
        </UFormField>
        <UButton
          type="submit"
          :form="formId"
          :disabled="blocker !== null"
          :loading="submitting"
          class="shrink-0 justify-center lg:h-10"
        >
          Create server
        </UButton>
      </div>
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
      <p v-else-if="blocker" class="text-sm text-muted">{{ blocker }}</p>
    </Panel>
  </aside>
</template>
