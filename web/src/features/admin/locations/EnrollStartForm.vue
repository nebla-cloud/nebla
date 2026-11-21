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
import { reactive, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import { useStartEnrollment, type EnrollmentInput } from './queries'

const emit = defineEmits<{ started: [number]; manual: []; cancel: [] }>()
const start = useStartEnrollment()
const form = reactive({ name: '', description: '' })
const nameError = ref<string | undefined>()
const formError = ref<string | null>(null)

async function submit() {
  formError.value = null
  nameError.value = /^[a-z0-9-]{1,32}$/.test(form.name)
    ? undefined
    : 'Use 1 to 32 lowercase letters, numbers or hyphens.'
  if (nameError.value) return
  const body: EnrollmentInput = { name: form.name }
  if (form.description.trim()) body.description = form.description.trim()
  try {
    emit('started', (await start.mutateAsync(body)).enrollment.id)
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <form class="flex max-w-2xl flex-col gap-5" novalidate @submit.prevent="submit">
    <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
    <UFormField
      label="Name"
      name="name"
      help="Lowercase letters, numbers and hyphens, up to 32. People see this when they pick where to run a server."
      :error="nameError"
    >
      <UInput v-model="form.name" name="name" autocomplete="off" class="w-full" />
    </UFormField>
    <UFormField label="Description" name="description" help="Optional. A line about where this is.">
      <UInput v-model="form.description" name="description" autocomplete="off" class="w-full" />
    </UFormField>
    <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-between">
      <UButton color="neutral" variant="ghost" class="justify-center" @click="emit('manual')">
        Connect manually
      </UButton>
      <div class="flex flex-col-reverse gap-2 sm:flex-row">
        <UButton color="neutral" variant="ghost" class="justify-center" @click="emit('cancel')">
          Cancel
        </UButton>
        <UButton type="submit" :loading="start.isPending.value" class="justify-center">
          Get setup command
        </UButton>
      </div>
    </div>
  </form>
</template>
