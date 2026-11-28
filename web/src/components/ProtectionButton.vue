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
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'

type Key = 'delete' | 'rebuild'
type Flags = Partial<Record<Key, boolean>>

const props = defineProps<{
  flags: { key: Key; label: string; description: string }[]
  value: Flags
  save: (changed: Flags) => Promise<unknown>
}>()
const emit = defineEmits<{ saved: [] }>()

const toast = useToast()
const open = ref(false)
const pending = ref(false)
const draft = ref<Flags>({})
const any = computed(() => props.flags.some((f) => props.value[f.key]))

watch(open, (v) => {
  if (v) draft.value = { ...props.value }
})

async function submit() {
  const changed: Flags = {}
  for (const f of props.flags) {
    const next = !!draft.value[f.key]
    if (next !== !!props.value[f.key]) changed[f.key] = next
  }
  if (Object.keys(changed).length === 0) {
    open.value = false
    return
  }
  pending.value = true
  try {
    await props.save(changed)
    open.value = false
    emit('saved')
  } catch (e) {
    toast.add({ title: "Couldn't save the change", description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UButton
    color="neutral"
    variant="outline"
    :icon="any ? 'i-lucide-lock' : 'i-lucide-lock-open'"
    @click="open = true"
  >
    Protection
  </UButton>
  <UModal v-model:open="open" title="Change protection">
    <template #body>
      <div class="flex flex-col gap-4">
        <UCheckbox
          v-for="f in flags"
          :key="f.key"
          v-model="draft[f.key]"
          :label="f.label"
          :description="f.description"
        />
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton :loading="pending" @click="submit">Save</UButton>
      </div>
    </template>
  </UModal>
</template>
