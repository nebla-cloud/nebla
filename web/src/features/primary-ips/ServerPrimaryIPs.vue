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
import { RouterLink } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Action, PrimaryIp, Server } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import Panel from '@/components/Panel.vue'
import { useCurrentProject } from '@/lib/project'
import { selectFullClass } from '@/lib/ui'
import {
  assignPrimaryIP,
  noPublic,
  OLD_SERVER,
  STOP_FIRST,
  TAKES_EFFECT,
  unassignPrimaryIP,
  usePrimaryIPs,
  useRefreshPrimaryIPs,
} from './queries'

const props = defineProps<{ server: Server; busy: boolean }>()
const emit = defineEmits<{ started: [action: Action] }>()

const toast = useToast()
const { projectId } = useCurrentProject()
const { data: all } = usePrimaryIPs()
const refresh = useRefreshPrimaryIPs()

const families = [
  { type: 'ipv4', label: 'IPv4' },
  { type: 'ipv6', label: 'IPv6' },
] as const

const stopped = computed(() => props.server.status === 'off')
const locked = computed(() => !stopped.value || props.busy)

const rows = computed(() =>
  families.map((f) => {
    const ip = props.server.public_net[f.type]
    return {
      ...f,
      current: ip ? ((all.value ?? []).find((p) => p.id === ip.id) ?? null) : null,
      address: ip?.ip ?? null,
    }
  }),
)

function unassignedOf(type: PrimaryIp['type']): PrimaryIp[] {
  return (all.value ?? []).filter(
    (p) =>
      p.type === type &&
      p.server === null &&
      p.status === 'active' &&
      p.location.id === props.server.location.id,
  )
}

const pending = ref(false)
const warnings = ref<string[]>([])

const unassigning = ref<(typeof families)[number]['type'] | null>(null)
const unassignTarget = computed(() => rows.value.find((r) => r.type === unassigning.value))
const unassignOpen = computed({
  get: () => unassigning.value !== null,
  set: (v) => {
    if (!v) unassigning.value = null
  },
})

async function unassign() {
  const target = unassignTarget.value?.current
  if (!target) return
  pending.value = true
  try {
    const res = await unassignPrimaryIP(target.id)
    warnings.value = res.warnings
    unassigning.value = null
    void refresh()
    emit('started', res.action)
  } catch (e) {
    unassigning.value = null
    toast.add({
      title: `Couldn't unassign ${target.name}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

const assigning = ref<(typeof families)[number]['type'] | null>(null)
const assignOpen = computed({
  get: () => assigning.value !== null,
  set: (v) => {
    if (!v) assigning.value = null
  },
})
const choices = computed(() => (assigning.value ? unassignedOf(assigning.value) : []))
const pickedId = ref<number | null>(null)
const assignError = ref<string | null>(null)
watch(assigning, () => {
  pickedId.value = null
  assignError.value = null
})

async function assign() {
  assignError.value = null
  if (pickedId.value === null) {
    assignError.value = 'Pick an IP.'
    return
  }
  pending.value = true
  try {
    const res = await assignPrimaryIP(pickedId.value, props.server.id)
    warnings.value = res.warnings
    assigning.value = null
    void refresh()
    emit('started', res.action)
  } catch (e) {
    assignError.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <Panel title="Primary IPs">
    <div class="flex flex-col gap-3 text-sm">
      <UAlert
        v-for="w in warnings"
        :key="w"
        color="warning"
        variant="subtle"
        icon="i-lucide-triangle-alert"
        :description="w"
      />
      <ul class="flex flex-col gap-2">
        <li
          v-for="r in rows"
          :key="r.type"
          :data-primary-ip="r.type"
          class="flex flex-wrap items-center justify-between gap-2"
        >
          <span class="flex min-w-0 flex-wrap items-baseline gap-x-3">
            <span class="w-12 text-muted">{{ r.label }}</span>
            <template v-if="r.address">
              <RouterLink
                v-if="r.current"
                :to="{ name: 'primary-ip', params: { projectId, primaryIpId: r.current.id } }"
                class="font-medium text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
                >{{ r.current.name }}</RouterLink
              >
              <span class="nb-mono wrap-anywhere text-muted">{{ r.address }}</span>
            </template>
            <span v-else class="text-muted">None</span>
          </span>
          <UButton
            v-if="r.address"
            color="neutral"
            variant="outline"
            size="xs"
            :disabled="locked"
            :aria-label="`Unassign ${r.label}`"
            @click="unassigning = r.type"
          >
            Unassign
          </UButton>
          <UButton
            v-else
            color="neutral"
            variant="outline"
            size="xs"
            :disabled="locked"
            :aria-label="`Assign ${r.label}`"
            @click="assigning = r.type"
          >
            Assign
          </UButton>
        </li>
      </ul>
      <p v-if="!stopped" class="text-muted">{{ STOP_FIRST }}</p>
    </div>

    <ConfirmDialog
      v-model:open="unassignOpen"
      :title="`Unassign ${unassignTarget?.current?.name ?? 'the primary IP'}?`"
      :body="`${noPublic(unassignTarget?.type ?? 'ipv4')} ${TAKES_EFFECT}${server.ip_change_reruns_cloud_init ? ` ${OLD_SERVER}` : ''}`"
      confirm-label="Unassign"
      :loading="pending"
      @confirm="unassign"
    />
    <UModal
      v-model:open="assignOpen"
      :title="`Assign a primary ${assigning === 'ipv6' ? 'IPv6' : 'IPv4'}`"
      :description="TAKES_EFFECT"
    >
      <template #body>
        <form
          id="assign-server-primary-ip"
          class="flex flex-col gap-4"
          novalidate
          @submit.prevent="assign"
        >
          <UAlert v-if="assignError" color="error" variant="subtle" :description="assignError" />
          <UAlert
            v-if="server.ip_change_reruns_cloud_init"
            color="warning"
            variant="subtle"
            icon="i-lucide-triangle-alert"
            :description="OLD_SERVER"
          />
          <p v-if="!choices.length" class="text-muted">
            No unassigned {{ assigning === 'ipv6' ? 'IPv6' : 'IPv4' }} addresses in
            {{ server.location.name }}. Create one on the Primary IPs page.
          </p>
          <UFormField v-else label="Primary IP" name="primary_ip">
            <select v-model="pickedId" name="primary_ip" :class="selectFullClass">
              <option :value="null" disabled>Pick an IP</option>
              <option v-for="p in choices" :key="p.id" :value="p.id">
                {{ p.name }} ({{ p.ip }})
              </option>
            </select>
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="assigning = null">Cancel</UButton>
          <UButton
            type="submit"
            form="assign-server-primary-ip"
            :loading="pending"
            :disabled="!choices.length"
          >
            Assign
          </UButton>
        </div>
      </template>
    </UModal>
  </Panel>
</template>
