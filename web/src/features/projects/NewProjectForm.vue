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
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import { useCreateProject } from './queries'

defineProps<{ autofocus?: boolean }>()

const router = useRouter()
const toast = useToast()
const create = useCreateProject()
const name = ref('')
const error = ref<string | null>(null)

async function submit() {
  error.value = null
  const value = name.value.trim()
  if (!value) {
    error.value = 'Give the project a name.'
    return
  }
  try {
    const p = await create.mutateAsync(value)
    toast.add({ title: `Created ${p.name}`, color: 'success' })
    await router.push(`/p/${p.id}/servers`)
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <form class="flex flex-col gap-3 sm:flex-row sm:items-start" novalidate @submit.prevent="submit">
    <UFormField label="Project name" name="name" class="flex-1" :error="error ?? undefined">
      <UInput v-model="name" name="name" autocomplete="off" class="w-full" :autofocus="autofocus" />
    </UFormField>
    <UButton type="submit" :loading="create.isPending.value" class="justify-center sm:mt-6">
      Create project
    </UButton>
  </form>
</template>
