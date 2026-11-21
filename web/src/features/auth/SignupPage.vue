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
import { RouterLink, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { setMe } from '@/lib/session'

const router = useRouter()
const form = reactive({ name: '', email: '', password: '' })
const busy = ref(false)
const error = ref<string | null>(null)
const emailTaken = ref(false)

async function submit() {
  if (busy.value) return
  error.value = null
  emailTaken.value = false
  if (form.password.length < 10) {
    error.value = 'Use at least 10 characters for the password.'
    return
  }
  busy.value = true
  try {
    const user = unwrap(
      await api.POST('/signup', {
        body: { name: form.name.trim(), email: form.email.trim(), password: form.password },
      }),
    )
    setMe(user)
    await router.replace('/')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'email_taken') emailTaken.value = true
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthLayout title="Create your account">
    <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <UAlert v-if="error" color="error" variant="subtle" :description="error">
        <template v-if="emailTaken" #description>
          {{ error }}
          <RouterLink :to="{ name: 'login' }" class="font-medium underline">Log in</RouterLink>
        </template>
      </UAlert>
      <UFormField label="Name" name="name">
        <UInput v-model="form.name" name="name" autocomplete="name" required class="w-full" />
      </UFormField>
      <UFormField label="Email" name="email">
        <UInput
          v-model="form.email"
          name="email"
          type="email"
          autocomplete="username"
          required
          class="w-full"
        />
      </UFormField>
      <UFormField label="Password" name="password" hint="At least 10 characters">
        <UInput
          v-model="form.password"
          name="password"
          type="password"
          autocomplete="new-password"
          required
          class="w-full"
        />
      </UFormField>
      <UButton type="submit" block size="lg" :loading="busy">Create account</UButton>
    </form>
    <template #after>
      Already have an account?
      <RouterLink :to="{ name: 'login' }" class="font-medium text-primary">Log in</RouterLink>
    </template>
  </AuthLayout>
</template>
