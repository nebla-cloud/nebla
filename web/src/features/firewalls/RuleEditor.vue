<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Action } from '@/api/types'
import Panel from '@/components/Panel.vue'
import * as ui from '@/lib/ui'
import ActionsProgress from './ActionsProgress.vue'
import { useSetRules, type Firewall } from './queries'
import {
  PROTOCOLS,
  blankRow,
  hasPort,
  rowError,
  toRows,
  toRule,
  type Direction,
  type RuleRow,
} from './rules'

const props = defineProps<{ firewall: Firewall; writable: boolean }>()

const toast = useToast()
const setRules = useSetRules()
const rows = ref<Record<Direction, RuleRow[]>>({ in: [], out: [] })
const saved = ref('')
const showErrors = ref(false)
const saveError = ref<string | null>(null)
const actions = ref<Action[]>([])

function reset() {
  rows.value = { in: toRows(props.firewall.rules, 'in'), out: toRows(props.firewall.rules, 'out') }
  saved.value = JSON.stringify(rows.value)
  showErrors.value = false
}
watch(() => props.firewall.rules, reset, { immediate: true })

const dirty = computed(() => JSON.stringify(rows.value) !== saved.value)
const sections = [
  { dir: 'in' as const, title: 'Inbound', ips: 'Sources' },
  { dir: 'out' as const, title: 'Outbound', ips: 'Destinations' },
]

function add(d: Direction) {
  rows.value[d].push(blankRow())
}
function remove(d: Direction, i: number) {
  rows.value[d].splice(i, 1)
}
const err = (d: Direction, i: number) => (showErrors.value ? rowError(rows.value[d][i]!) : {})

async function save() {
  saveError.value = null
  const all = [...rows.value.in, ...rows.value.out]
  if (all.some((r) => Object.keys(rowError(r)).length)) {
    showErrors.value = true
    return
  }
  const rules = [
    ...rows.value.in.map((r) => toRule(r, 'in')),
    ...rows.value.out.map((r) => toRule(r, 'out')),
  ]
  try {
    actions.value = await setRules.mutateAsync({ id: props.firewall.id, rules })
    saved.value = JSON.stringify(rows.value)
    showErrors.value = false
    toast.add({ title: 'Rules saved', color: 'success' })
  } catch (e) {
    saveError.value = errorMessage(e)
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <Panel v-for="s in sections" :key="s.dir" :title="s.title">
      <template v-if="writable" #header-actions>
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-plus"
          :aria-label="`Add ${s.title.toLowerCase()} rule`"
          @click="add(s.dir)"
        >
          Add rule
        </UButton>
      </template>
      <p v-if="!rows[s.dir].length" class="text-sm text-muted">
        {{ s.dir === 'in' ? 'No inbound traffic is allowed.' : 'No outbound traffic is allowed.' }}
      </p>
      <div v-else :class="ui.tableWrap">
        <table :class="ui.table">
          <thead :class="ui.thead">
            <tr>
              <th :class="ui.th">Protocol</th>
              <th :class="ui.th">Port</th>
              <th :class="ui.th">{{ s.ips }}</th>
              <th :class="ui.th">Description</th>
              <th v-if="writable" :class="ui.th"><span class="sr-only">Remove</span></th>
            </tr>
          </thead>
          <tbody :class="ui.tbody">
            <tr v-for="(r, i) in rows[s.dir]" :key="i" :class="ui.tr">
              <template v-if="writable">
                <td :class="ui.td">
                  <select
                    v-model="r.protocol"
                    :name="`${s.dir}-${i}-protocol`"
                    :aria-label="`Protocol, ${s.title} rule ${i + 1}`"
                    :class="ui.selectClass"
                  >
                    <option v-for="p in PROTOCOLS" :key="p" :value="p">{{ p }}</option>
                  </select>
                </td>
                <td :class="ui.td">
                  <UInput
                    v-if="hasPort(r.protocol)"
                    v-model="r.port"
                    :name="`${s.dir}-${i}-port`"
                    :aria-label="`Port, ${s.title} rule ${i + 1}`"
                    :color="err(s.dir, i).port ? 'error' : undefined"
                    :highlight="!!err(s.dir, i).port"
                    placeholder="22"
                    class="w-28 font-mono"
                  />
                  <span v-else class="text-muted">Any</span>
                  <p v-if="err(s.dir, i).port" class="mt-1 text-xs text-error">
                    {{ err(s.dir, i).port }}
                  </p>
                </td>
                <td :class="ui.td">
                  <UInput
                    v-model="r.ips"
                    :name="`${s.dir}-${i}-ips`"
                    :aria-label="`${s.ips}, ${s.title} rule ${i + 1}`"
                    :highlight="!!err(s.dir, i).ips"
                    :color="err(s.dir, i).ips ? 'error' : undefined"
                    class="w-full min-w-48 font-mono"
                  />
                  <p v-if="err(s.dir, i).ips" class="mt-1 text-xs text-error">
                    {{ err(s.dir, i).ips }}
                  </p>
                </td>
                <td :class="ui.td">
                  <UInput
                    v-model="r.description"
                    :name="`${s.dir}-${i}-description`"
                    :aria-label="`Description, ${s.title} rule ${i + 1}`"
                    class="w-full"
                  />
                  <p v-if="err(s.dir, i).description" class="mt-1 text-xs text-error">
                    {{ err(s.dir, i).description }}
                  </p>
                </td>
                <td :class="ui.td">
                  <UButton
                    color="neutral"
                    variant="ghost"
                    size="sm"
                    icon="i-lucide-x"
                    :aria-label="`Remove ${s.title.toLowerCase()} rule ${i + 1}`"
                    @click="remove(s.dir, i)"
                  />
                </td>
              </template>
              <template v-else>
                <td :class="ui.td" class="font-mono">{{ r.protocol }}</td>
                <td :class="ui.td" class="font-mono">{{ hasPort(r.protocol) ? r.port : 'Any' }}</td>
                <td :class="ui.td" class="font-mono">{{ r.ips }}</td>
                <td :class="ui.td" class="text-muted">{{ r.description }}</td>
              </template>
            </tr>
          </tbody>
        </table>
      </div>
    </Panel>

    <UAlert v-if="saveError" color="error" variant="subtle" :description="saveError" />
    <div v-if="writable" class="flex justify-end gap-2">
      <UButton v-if="dirty" color="neutral" variant="ghost" @click="reset">Discard</UButton>
      <UButton :disabled="!dirty" :loading="setRules.isPending.value" @click="save">
        Save rules
      </UButton>
    </div>
    <ActionsProgress :actions="actions" />
  </div>
</template>
