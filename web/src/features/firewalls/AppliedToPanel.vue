<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { refDebounced } from '@vueuse/core'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action } from '@/api/types'
import ChoiceCard from '@/components/ChoiceCard.vue'
import LabelChip from '@/components/LabelChip.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useServers } from '@/features/servers/queries'
import { useCurrentProject } from '@/lib/project'
import { selectClass } from '@/lib/ui'
import ActionsProgress from './ActionsProgress.vue'
import { useApplyTo, useRemoveFrom, type Firewall, type FirewallResource } from './queries'

const props = defineProps<{ firewall: Firewall; writable: boolean }>()

const { projectId } = useCurrentProject()
const { data: servers } = useServers()
const byId = computed(() => new Map((servers.value ?? []).map((s) => [s.id, s])))
const applyTo = useApplyTo()
const removeFrom = useRemoveFrom()
const actions = ref<Action[]>([])
const error = ref<string | null>(null)

const direct = computed(() => props.firewall.applied_to.filter((r) => r.type === 'server'))
const selectors = computed(() => props.firewall.applied_to.filter((r) => r.type === 'label_selector'))
const nameOf = (id: number) => byId.value.get(id)?.name ?? `Server ${id}`
const matches = (n: number) => `Matches ${n} server${n === 1 ? '' : 's'}`

const adding = ref(false)
const target = ref<'server' | 'label_selector'>('server')
const serverId = ref('')
const selector = ref('')
const formError = ref<string | null>(null)
const debounced = refDebounced(selector, 250)
const previewing = computed(() => target.value === 'label_selector' && !!debounced.value.trim())
const preview = useServers(computed(() => (previewing.value ? debounced.value : '')))
const candidates = computed(() =>
  (servers.value ?? []).filter((s) => !direct.value.some((r) => r.server?.id === s.id)),
)

watch(adding, (v) => {
  if (v) return
  target.value = 'server'
  serverId.value = ''
  selector.value = ''
  formError.value = null
})

async function run(fn: () => Promise<Action[]>): Promise<boolean> {
  error.value = null
  try {
    actions.value = await fn()
    return true
  } catch (e) {
    error.value = errorMessage(e)
    return false
  }
}

async function submit() {
  formError.value = null
  let res: FirewallResource
  if (target.value === 'server') {
    if (!serverId.value) return void (formError.value = 'Pick a server.')
    res = { type: 'server', server: { id: Number(serverId.value) } }
  } else {
    const s = selector.value.trim()
    if (!s) return void (formError.value = 'Enter a label selector, like role=web.')
    res = { type: 'label_selector', label_selector: { selector: s } }
  }
  try {
    actions.value = await applyTo.mutateAsync({ id: props.firewall.id, resources: [res] })
    adding.value = false
  } catch (e) {
    formError.value = errorMessage(e)
  }
}

function remove(r: FirewallResource) {
  const res: FirewallResource =
    r.type === 'server'
      ? { type: 'server', server: { id: r.server!.id } }
      : { type: 'label_selector', label_selector: { selector: r.label_selector!.selector } }
  void run(() => removeFrom.mutateAsync({ id: props.firewall.id, resources: [res] }))
}
</script>

<template>
  <Panel title="Applied to">
    <template v-if="writable" #header-actions>
      <UButton color="neutral" variant="outline" size="sm" icon="i-lucide-plus" @click="adding = true">
        Add
      </UButton>
    </template>
    <div class="flex flex-col gap-3">
      <p v-if="!firewall.applied_to.length" class="text-sm text-muted">
        Not applied to any server yet.
      </p>
      <ListGroup v-else label="Applied to">
        <ListRow v-for="r in direct" :key="`s${r.server!.id}`">
          <template v-if="byId.get(r.server!.id)" #leading>
            <StatusDot :status="byId.get(r.server!.id)!.status" />
          </template>
          <RouterLink
            :to="{ name: 'server', params: { projectId, serverId: r.server!.id } }"
            class="hover:underline focus-visible:outline-2 focus-visible:outline-primary"
          >
            {{ nameOf(r.server!.id) }}
          </RouterLink>
          <template #meta>Server</template>
          <template v-if="writable" #trailing>
            <UButton
              color="neutral"
              variant="ghost"
              size="sm"
              icon="i-lucide-x"
              :aria-label="`Remove ${nameOf(r.server!.id)}`"
              @click="remove(r)"
            />
          </template>
        </ListRow>
        <ListRow v-for="r in selectors" :key="`l${r.label_selector!.selector}`">
          <LabelChip :k="r.label_selector!.selector" v="" />
          <template #meta>{{ matches(r.applied_to_resources?.length ?? 0) }}</template>
          <template v-if="writable" #trailing>
            <UButton
              color="neutral"
              variant="ghost"
              size="sm"
              icon="i-lucide-x"
              :aria-label="`Remove ${r.label_selector!.selector}`"
              @click="remove(r)"
            />
          </template>
        </ListRow>
      </ListGroup>
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
      <ActionsProgress :actions="actions" />
    </div>

    <UModal v-model:open="adding" title="Apply firewall" description="Pick a server, or every server with matching labels.">
      <template #body>
        <form id="apply-firewall" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <div class="grid gap-2 sm:grid-cols-2">
            <ChoiceCard v-model="target" type="radio" name="target" value="server">
              A server
            </ChoiceCard>
            <ChoiceCard v-model="target" type="radio" name="target" value="label_selector">
              Servers by label
              <template #meta>Includes servers added later.</template>
            </ChoiceCard>
          </div>
          <UFormField
            v-if="target === 'server'"
            label="Server"
            name="server"
            :error="formError ?? undefined"
          >
            <select v-model="serverId" name="server" :class="selectClass">
              <option value="" disabled>Pick a server</option>
              <option v-for="s in candidates" :key="s.id" :value="String(s.id)">{{ s.name }}</option>
            </select>
          </UFormField>
          <template v-else>
            <UFormField
              label="Label selector"
              name="selector"
              help="Like role=web, or env!=prod."
              :error="formError ?? undefined"
            >
              <UInput v-model="selector" name="selector" autocomplete="off" class="w-full font-mono" />
            </UFormField>
            <div v-if="previewing" data-preview aria-live="polite" class="flex flex-col gap-2 text-sm">
              <p v-if="preview.error.value" class="text-error">
                {{ errorMessage(preview.error.value) }}
              </p>
              <p v-else-if="preview.isPending.value" class="text-muted">Checking…</p>
              <template v-else>
                <p class="text-muted">{{ matches(preview.data.value?.length ?? 0) }} right now</p>
                <ListGroup v-if="preview.data.value?.length" label="Matching servers">
                  <ListRow v-for="s in preview.data.value" :key="s.id">
                    <template #leading><StatusDot :status="s.status" /></template>
                    {{ s.name }}
                  </ListRow>
                </ListGroup>
              </template>
            </div>
          </template>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="adding = false">Cancel</UButton>
          <UButton type="submit" form="apply-firewall" :loading="applyTo.isPending.value">
            Apply
          </UButton>
        </div>
      </template>
    </UModal>
  </Panel>
</template>
