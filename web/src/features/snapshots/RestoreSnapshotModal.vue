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
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action, Snapshot } from '@/api/types'
import { identityWarning, restoreText } from './copy'
import { restoreSnapshot } from './queries'

const props = defineProps<{ snapshot: Snapshot; serverName: string }>()
const emit = defineEmits<{ started: [action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const error = ref<string | null>(null)
const pending = ref(false)

const body = computed(() =>
  props.snapshot.restore_keeps_identity === false
    ? `${restoreText(props.serverName, props.snapshot)} ${identityWarning(props.serverName)}`
    : restoreText(props.serverName, props.snapshot),
)

watch(open, () => (error.value = null))

async function submit() {
  error.value = null
  pending.value = true
  try {
    emit('started', await restoreSnapshot(props.snapshot))
    open.value = false
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal v-model:open="open" :title="`Restore ${serverName}`" :description="body">
    <template v-if="error" #body>
      <UAlert color="error" variant="subtle" :description="error" />
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton color="error" :loading="pending" @click="submit">Restore</UButton>
      </div>
    </template>
  </UModal>
</template>
