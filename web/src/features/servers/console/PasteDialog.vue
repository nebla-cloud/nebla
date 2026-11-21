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
import UFormField from '@nuxt/ui/components/FormField.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { untypeable } from './keys'

const MAX = 4000

const emit = defineEmits<{ submit: [text: string] }>()
const open = defineModel<boolean>('open', { default: false })

const text = ref('')
const refused = ref(false)
const count = computed(() => [...text.value].length)
const over = computed(() => count.value > MAX)
const bad = computed(() => untypeable(text.value))
const shown = (chars: string[]) =>
  chars.map((c) =>
    c.trim() ? c : `U+${c.codePointAt(0)!.toString(16).toUpperCase().padStart(4, '0')}`,
  )
const problem = computed(() => {
  if (bad.value.length) {
    const list = shown(bad.value.slice(0, 10)).join(' ')
    const more = bad.value.length > 10 ? ` and ${bad.value.length - 10} more` : ''
    return `The console can't type these characters: ${list}${more}. Remove them and try again.`
  }
  if (refused.value) return "That's more than 4,000 characters. Shorten it and try again."
  return false
})

watch(open, (v) => {
  if (!v) {
    text.value = ''
    refused.value = false
  }
})
watch(text, () => {
  refused.value = false
})

function submit() {
  if (over.value) {
    refused.value = true
    return
  }
  if (bad.value.length) return
  if (!text.value) return
  emit('submit', text.value)
  open.value = false
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Paste text"
    description="Nebla types this into the server one key at a time. It can type letters, numbers, keyboard symbols, Return and Tab, up to 4,000 characters."
  >
    <template #body>
      <form id="console-paste" class="flex flex-col gap-2" @submit.prevent="submit">
        <UFormField label="Text to type" name="paste_text" :error="problem">
          <UTextarea
            v-model="text"
            name="paste_text"
            :rows="6"
            autocomplete="off"
            spellcheck="false"
            class="w-full font-mono"
            autofocus
          />
        </UFormField>
        <p class="text-xs" :class="over ? 'text-error' : 'text-muted'" aria-live="polite">
          {{ count.toLocaleString('en-US') }} of 4,000 characters
        </p>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="console-paste">Type it</UButton>
      </div>
    </template>
  </UModal>
</template>
