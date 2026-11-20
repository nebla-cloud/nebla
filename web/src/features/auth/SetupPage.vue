<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { useSession } from '@/lib/session'

const router = useRouter()
const session = useSession()

const form = reactive({ name: '', email: '', password: '' })
const busy = ref(false)
const error = ref<string | null>(null)

async function submit() {
  if (busy.value) return
  if (!form.name.trim() || !form.email.trim() || !form.password) {
    error.value = 'Fill in your name, email and password.'
    return
  }
  busy.value = true
  error.value = null
  try {
    unwrap(
      await api.POST('/setup', {
        body: { name: form.name.trim(), email: form.email.trim(), password: form.password },
      }),
    )
    unwrap(
      await api.POST('/auth/login', {
        body: { email: form.email.trim(), password: form.password },
      }),
    )
    await session.refresh()
    await router.replace('/')
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthLayout
    title="Create the admin account"
    description="You're the first person here, so this account becomes the panel admin."
  >
    <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
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
      <UFormField label="Password" name="password">
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
  </AuthLayout>
</template>
