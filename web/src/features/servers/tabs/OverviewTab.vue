<script setup lang="ts">
import { computed, inject, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import { actionDot, actionStatus, actionTitle, relativeTime } from '@/components/actionLabel'
import ConfirmByName from '@/components/ConfirmByName.vue'
import MetricCard from '@/components/MetricCard.vue'
import Panel from '@/components/Panel.vue'
import Sparkline from '@/components/Sparkline.vue'
import { useActions } from '@/features/activity/queries'
import { formatDate, typeSpec } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import LabelChips from '../LabelChips.vue'
import LabelEditor from '../LabelEditor.vue'
import { formatPercent, formatRate } from '../metrics/metrics'
import { useLiveMetrics } from '../metrics/useLiveMetrics'
import { osLabel } from '../osLabel'
import { serverBusyKey } from '../shell'

const NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const toast = useToast()
const qc = useQueryClient()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)

const serverId = computed(() => props.server.id)
const running = computed(() => props.server.status === 'running')
const live = useLiveMetrics(serverId, running)

const DASH = '—'
const rate = (v: number | null) => (v == null ? DASH : formatRate(v))
// Reads "{a} / {b}": the second figure sits in the card's smaller unit slot, as in the mockup.
const pair = (a: number | null, b: number | null) =>
  a == null && b == null ? { value: DASH } : { value: rate(a), unit: `/ ${rate(b)}` }
const cards = computed(() => {
  const ts = live.series.value
  return [
    {
      key: 'cpu',
      label: 'CPU',
      value: live.cpu.value == null ? DASH : formatPercent(live.cpu.value),
      lines: [{ points: ts.cpu ?? [], tone: 'busy' as const }],
    },
    {
      key: 'network',
      label: 'Network',
      ...pair(live.netIn.value, live.netOut.value),
      lines: [
        { points: ts['network.in'] ?? [], tone: 'run' as const },
        { points: ts['network.out'] ?? [], tone: 'err' as const },
      ],
    },
    {
      key: 'disk',
      label: 'Disk activity',
      ...pair(live.diskRead.value, live.diskWrite.value),
      lines: [
        { points: ts['disk.read'] ?? [], tone: 'busy' as const },
        { points: ts['disk.write'] ?? [], tone: 'err' as const },
      ],
    },
  ]
})

const activity = useActions({ serverId })
const recent = computed(() => activity.data.value?.pages[0]?.actions.slice(0, 3) ?? [])

const rows = computed(() => {
  const s = props.server
  const list: { label: string; value: string; ip?: boolean; sub?: string }[] = []
  if (s.public_net.ipv4) list.push({ label: 'IPv4', value: s.public_net.ipv4.ip, ip: true })
  if (s.public_net.ipv6) list.push({ label: 'IPv6', value: s.public_net.ipv6.ip, ip: true })
  list.push(
    // The slug stays as a second, muted value: e2e reads it from a definition.
    {
      label: 'Image',
      value: osLabel(s.image),
      sub: osLabel(s.image) === s.image.name ? undefined : s.image.name,
    },
    { label: 'Type', value: `${s.server_type.name}: ${typeSpec(s.server_type)}` },
    { label: 'Location', value: s.location.name },
    { label: 'Created', value: formatDate(s.created) },
  )
  return list
})

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast.add({ title: 'Copied', color: 'success' })
  } catch {
    toast.add({ title: "Couldn't copy", color: 'error' })
  }
}

const editingLabels = ref(false)
const renaming = ref(false)
const newName = ref('')
const renameError = ref<string | null>(null)
const renameBusy = ref(false)

function startRename() {
  newName.value = props.server.name
  renameError.value = null
  renaming.value = true
}

async function submitRename() {
  const name = newName.value.trim()
  if (!NAME_PATTERN.test(name)) {
    renameError.value =
      'Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.'
    return
  }
  renameBusy.value = true
  try {
    const s: Server = unwrap(
      await api.PUT('/servers/{id}', {
        params: { path: { id: props.server.id } },
        body: { name },
      }),
    )
    qc.setQueryData(['server', projectId.value, s.id], s)
    await qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
    toast.add({ title: `Renamed to ${s.name}`, color: 'success' })
    renaming.value = false
  } catch (e) {
    renameError.value = errorMessage(e)
  } finally {
    renameBusy.value = false
  }
}

const confirmOpen = ref(false)
const deleting = ref(false)
const deleteError = ref<string | null>(null)

async function confirmDelete() {
  deleting.value = true
  deleteError.value = null
  try {
    const res = unwrap(
      await api.DELETE('/servers/{id}', { params: { path: { id: props.server.id } } }),
    )
    confirmOpen.value = false
    emit('started', res.action)
  } catch (e) {
    confirmOpen.value = false
    deleteError.value = errorMessage(e)
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div v-if="running" class="grid gap-4 sm:grid-cols-3">
      <MetricCard
        v-for="c in cards"
        :key="c.key"
        :data-metric="c.key"
        :label="c.label"
        :value="c.value"
        :unit="c.unit"
      >
        <template #chart>
          <div class="grid *:col-start-1 *:row-start-1">
            <Sparkline
              v-for="l in c.lines"
              :key="l.tone"
              :points="l.points"
              :width="200"
              :height="40"
              :tone="l.tone"
            />
          </div>
        </template>
      </MetricCard>
    </div>
    <p v-else class="text-sm text-muted">Graphs appear once it's running</p>

    <div class="grid items-start gap-4 lg:grid-cols-[1.3fr_1fr]">
      <Panel>
        <div class="flex flex-col gap-4">
          <form
            v-if="renaming"
            class="flex flex-wrap items-start gap-2"
            novalidate
            @submit.prevent="submitRename"
          >
            <UFormField name="name" label="Server name" :error="renameError ?? undefined">
              <UInput v-model="newName" name="name" autocomplete="off" autofocus />
            </UFormField>
            <div class="flex gap-2 sm:mt-6">
              <UButton type="submit" :loading="renameBusy">Save</UButton>
              <UButton color="neutral" variant="ghost" @click="renaming = false">Cancel</UButton>
            </div>
          </form>

          <dl
            class="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-3 text-sm sm:grid-cols-[7.5rem_minmax(0,1fr)]"
          >
            <dt class="text-muted">Name</dt>
            <dd class="flex flex-wrap items-center gap-2 font-medium break-all text-highlighted">
              {{ server.name }}
              <UButton
                v-if="!renaming"
                color="neutral"
                variant="link"
                size="xs"
                icon="i-lucide-pencil"
                @click="startRename"
              >
                Rename
              </UButton>
            </dd>
            <template v-for="r in rows" :key="r.label">
              <dt class="text-muted">{{ r.label }}</dt>
              <dd class="flex flex-wrap items-center gap-2 font-medium break-all text-highlighted">
                <span v-if="r.ip" class="font-mono">{{ r.value }}</span>
                <template v-else>{{ r.value }}</template>
                <UButton
                  v-if="r.ip"
                  color="neutral"
                  variant="link"
                  size="xs"
                  :aria-label="`Copy ${r.label}`"
                  @click="copy(r.value)"
                >
                  Copy
                </UButton>
              </dd>
              <dd v-if="r.sub" class="col-start-2 -mt-2.5 text-xs break-all text-muted">
                {{ r.sub }}
              </dd>
            </template>
            <dt class="text-muted">Labels</dt>
            <dd class="flex flex-wrap items-center gap-2">
              <template v-if="!editingLabels">
                <LabelChips :labels="server.labels" :max="64" />
                <UButton
                  color="neutral"
                  variant="link"
                  size="xs"
                  icon="i-lucide-tag"
                  @click="editingLabels = true"
                >
                  Edit labels
                </UButton>
              </template>
            </dd>
          </dl>
          <LabelEditor
            v-if="editingLabels"
            :server="server"
            @saved="editingLabels = false"
            @cancel="editingLabels = false"
          />
        </div>
      </Panel>

      <Panel title="Recent activity">
        <template #header-actions>
          <RouterLink
            :to="{ name: 'server-activity', params: { projectId, serverId: server.id } }"
            class="text-sm font-medium text-primary hover:underline"
            >View all</RouterLink
          >
        </template>
        <p v-if="activity.isPending.value" class="text-sm text-muted">Loading activity</p>
        <p v-else-if="activity.error.value" role="alert" class="text-sm text-error">
          {{ errorMessage(activity.error.value) }}
        </p>
        <p v-else-if="!recent.length" class="text-sm text-muted">Nothing has happened here yet.</p>
        <ul v-else class="flex flex-col gap-2.5 text-sm">
          <li v-for="a in recent" :key="a.id" class="flex flex-wrap items-baseline gap-x-2">
            <span class="font-medium text-highlighted">{{ actionTitle(a.command) }}</span>
            <span class="inline-flex items-center gap-1.5 text-default">
              <span aria-hidden="true" class="size-2 shrink-0 rounded-full" :class="actionDot(a)" />
              {{ actionStatus(a) }}
            </span>
            <time
              class="ms-auto text-muted"
              :datetime="a.started"
              :title="new Date(a.started).toLocaleString('en-GB')"
              >{{ relativeTime(a.started) }}</time
            >
          </li>
        </ul>
        <footer class="mt-4 flex flex-col gap-3 border-t border-default pt-4">
          <UAlert v-if="deleteError" color="error" variant="subtle" :description="deleteError" />
          <div class="flex flex-wrap items-center justify-between gap-3">
            <span class="text-sm text-muted">Delete this server and free its IP addresses.</span>
            <UButton color="error" variant="outline" :disabled="busy" @click="confirmOpen = true">
              Delete server
            </UButton>
          </div>
        </footer>
      </Panel>
      <ConfirmByName
        v-model:open="confirmOpen"
        :name="server.name"
        :title="`Delete ${server.name}?`"
        confirm-label="Delete server"
        :loading="deleting"
        @confirm="confirmDelete"
      />
    </div>
  </div>
</template>
