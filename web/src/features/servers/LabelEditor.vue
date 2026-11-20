<script setup lang="ts">
import { ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Server } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { validateLabels, type LabelRow } from './labels'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()

const toast = useToast()
const qc = useQueryClient()
const { projectId } = useCurrentProject()

const rows = ref<LabelRow[]>(
  Object.entries(props.server.labels)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ key, value })),
)
const errors = ref<Record<number, string>>({})
const formError = ref<string | null>(null)
const saving = ref(false)

function add() {
  rows.value.push({ key: '', value: '' })
}
function remove(i: number) {
  rows.value.splice(i, 1)
  errors.value = {}
}

async function save() {
  formError.value = null
  const v = validateLabels(rows.value)
  if (!v.ok) {
    errors.value = v.errors
    if (v.errors[64]) formError.value = v.errors[64]
    return
  }
  errors.value = {}
  saving.value = true
  try {
    const s: Server = unwrap(
      await api.PUT('/servers/{id}', {
        params: { path: { id: props.server.id } },
        body: { labels: v.labels },
      }),
    )
    qc.setQueryData(['server', projectId.value, s.id], s)
    await Promise.all([
      qc.invalidateQueries({ queryKey: ['server', projectId.value, s.id] }),
      qc.invalidateQueries({ queryKey: ['servers', projectId.value] }),
    ])
    toast.add({ title: 'Labels saved', color: 'success' })
    emit('saved')
  } catch (e) {
    formError.value = errorMessage(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-3" novalidate @submit.prevent="save">
    <p v-if="!rows.length" class="text-sm text-muted">No labels yet.</p>
    <div v-for="(r, i) in rows" :key="i" class="flex flex-wrap items-start gap-2">
      <UFormField :error="errors[i]" class="min-w-0 flex-1 basis-40">
        <UInput
          v-model="r.key"
          :name="`key-${i}`"
          placeholder="Key"
          :aria-label="`Label key ${i + 1}`"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
      <UInput
        v-model="r.value"
        :name="`value-${i}`"
        placeholder="Value"
        :aria-label="`Label value ${i + 1}`"
        autocomplete="off"
        class="min-w-0 flex-1 basis-40"
      />
      <UButton
        color="neutral"
        variant="ghost"
        icon="i-lucide-x"
        :aria-label="`Remove label ${i + 1}`"
        @click="remove(i)"
      />
    </div>
    <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
    <div class="flex flex-wrap gap-2">
      <UButton color="neutral" variant="outline" icon="i-lucide-plus" @click="add">
        Add label
      </UButton>
      <div class="flex gap-2 sm:ml-auto">
        <UButton type="submit" :loading="saving">Save labels</UButton>
        <UButton color="neutral" variant="ghost" @click="emit('cancel')">Cancel</UButton>
      </div>
    </div>
  </form>
</template>
