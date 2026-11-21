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
import { computed, ref, useId } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import TerminalBlock from './TerminalBlock.vue'
import { errorMessage } from '@/api/errors'
import { unavailable, useAdminAction } from '@/features/admin/activity/queries'
import { useCopy } from '@/lib/useCopy'

const props = defineProps<{ actionId: number; detail?: { code: string; detail: string } }>()

const open = ref(false)
const id = useId()
const block = ref<HTMLElement | null>(null)
const { copied, copy } = useCopy()

const q = useAdminAction(
  computed(() => props.actionId),
  computed(() => open.value && !props.detail),
)

const shown = computed(() => {
  if (props.detail) return props.detail
  const e = q.data.value?.error
  return e ? { code: e.code, detail: e.detail } : null
})
const missing = computed(
  () => (q.error.value && unavailable(q.error.value)) || (q.isSuccess.value && !shown.value),
)

function onCopy() {
  if (shown.value) void copy(`${shown.value.code}\n${shown.value.detail}`, block.value)
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <UButton
      color="neutral"
      variant="link"
      size="sm"
      class="self-start px-0"
      :icon="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
      :aria-expanded="open"
      :aria-controls="id"
      @click="open = !open"
    >
      Technical details
    </UButton>
    <div v-if="open" :id="id" class="flex flex-col gap-2">
      <p v-if="missing" class="text-sm text-muted">Details aren't available.</p>
      <p v-else-if="q.error.value" role="alert" class="text-sm text-error">
        {{ errorMessage(q.error.value) }}
      </p>
      <template v-else-if="shown">
        <div ref="block">
          <TerminalBlock class="font-mono break-words">
            <div class="font-semibold">{{ shown.code }}</div>
            <div>{{ shown.detail }}</div>
          </TerminalBlock>
        </div>
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          class="self-start"
          :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
          @click="onCopy"
        >
          {{ copied ? 'Copied' : 'Copy' }}
        </UButton>
      </template>
      <p v-else role="status" class="text-sm text-muted">Loading details</p>
    </div>
  </div>
</template>
