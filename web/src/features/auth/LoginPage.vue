<script setup lang="ts">
import { nextTick, reactive, ref, useTemplateRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { safeNext } from '@/router/guards'
import { setMe } from '@/lib/session'

const route = useRoute()
const router = useRouter()

const form = reactive({ email: '', password: '', totp: '' })
const needsTotp = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const totpInput = useTemplateRef<{ inputRef?: HTMLInputElement }>('totpInput')

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = null
  try {
    const body = needsTotp.value
      ? { email: form.email, password: form.password, totp_code: form.totp.trim() }
      : { email: form.email, password: form.password }
    const user = unwrap(await api.POST('/auth/login', { body }))
    setMe(user)
    await router.replace(safeNext(route.query.next) ?? '/')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'totp_required' && !needsTotp.value) {
      needsTotp.value = true
      await nextTick()
      totpInput.value?.inputRef?.focus()
    } else {
      error.value = errorMessage(e)
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthLayout
    title="Log in"
    :description="needsTotp ? 'Enter the 6-digit code from your authenticator app.' : undefined"
  >
    <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
      <UFormField label="Email" name="email">
        <UInput
          v-model="form.email"
          name="email"
          type="email"
          autocomplete="username"
          required
          class="w-full"
          :disabled="needsTotp"
        />
      </UFormField>
      <UFormField label="Password" name="password">
        <UInput
          v-model="form.password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
          class="w-full"
          :disabled="needsTotp"
        />
      </UFormField>
      <UFormField v-if="needsTotp" label="Authentication code" name="totp_code">
        <UInput
          ref="totpInput"
          v-model="form.totp"
          name="totp_code"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="8"
          required
          class="w-full"
        />
      </UFormField>
      <UButton type="submit" block size="lg" :loading="busy">Log in</UButton>
    </form>
  </AuthLayout>
</template>
