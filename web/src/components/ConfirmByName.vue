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
import UModal from '@nuxt/ui/components/Modal.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'

const props = defineProps<{
  name: string
  title: string
  confirmLabel: string
  loading?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ confirm: [] }>()
const open = defineModel<boolean>('open', { default: false })

const typed = ref('')
const matches = computed(() => typed.value === props.name)

watch(open, (v) => {
  if (!v) typed.value = ''
})

function submit() {
  if (matches.value && !props.loading) emit('confirm')
}
</script>

<template>
  <UModal v-model:open="open" :title="title" :description="`This can't be undone.`">
    <slot />
    <template #body>
      <form id="confirm-by-name" class="flex flex-col gap-3" @submit.prevent="submit">
        <slot name="intro" />
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <UFormField name="confirm_name">
          <template #label>
            Type
            <span class="nb-mono rounded-full bg-muted px-2 py-0.5 text-xs text-default">{{ name }}</span>
            to confirm
          </template>
          <UInput
            v-model="typed"
            name="confirm_name"
            autocomplete="off"
            spellcheck="false"
            class="w-full"
            autofocus
          />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton
          type="submit"
          form="confirm-by-name"
          color="error"
          :disabled="!matches"
          :loading="loading"
        >
          {{ confirmLabel }}
        </UButton>
      </div>
    </template>
  </UModal>
</template>
