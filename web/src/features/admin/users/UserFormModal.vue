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
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import { checkboxClass } from '@/lib/ui'
import { useSession } from '@/lib/session'
import { useCreateUser, useUpdateUser, type AdminUser } from './queries'

const props = defineProps<{ user?: AdminUser }>()
const emit = defineEmits<{ saved: [user: AdminUser] }>()
const open = defineModel<boolean>('open', { default: false })

const { me } = useSession()
const create = useCreateUser()
const update = useUpdateUser()

const name = ref('')
const email = ref('')
const password = ref('')
const isAdmin = ref(false)
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

const isSelf = computed(() => props.user !== undefined && props.user.id === me.value?.id)
const emailChanged = computed(
  () =>
    props.user !== undefined && email.value.trim().toLowerCase() !== props.user.email.toLowerCase(),
)
const pending = computed(() => create.isPending.value || update.isPending.value)

watch(
  open,
  (v) => {
    if (!v) return
    name.value = props.user?.name ?? ''
    email.value = props.user?.email ?? ''
    password.value = ''
    isAdmin.value = props.user?.is_admin ?? false
    errors.value = {}
    formError.value = null
  },
  { immediate: true },
)

async function submit() {
  formError.value = null
  const e: Record<string, string> = {}
  if (!name.value.trim()) e.name = 'Enter a name.'
  if (!email.value.includes('@')) e.email = 'Enter an email address, like sam@example.com.'
  if (!props.user && password.value.length < 10) e.password = 'Use at least 10 characters.'
  errors.value = e
  if (Object.keys(e).length) return
  const base = { name: name.value.trim(), email: email.value.trim() }
  try {
    const saved = props.user
      ? await update.mutateAsync({
          id: props.user.id,
          body: { ...base, is_admin: isSelf.value ? props.user.is_admin : isAdmin.value },
        })
      : await create.mutateAsync({ ...base, password: password.value, is_admin: isAdmin.value })
    emit('saved', saved)
    open.value = false
  } catch (err) {
    formError.value = errorMessage(err)
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="user ? `Edit ${user.name || user.email}` : 'New user'"
    :description="
      user ? 'Change their name, email or role.' : 'Create an account they can log in with.'
    "
  >
    <template #body>
      <form id="user-form" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <UFormField label="Name" name="name" :error="errors.name">
          <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
        </UFormField>
        <UFormField label="Email" name="email" :error="errors.email">
          <UInput v-model="email" name="email" type="email" autocomplete="off" class="w-full" />
        </UFormField>
        <p v-if="emailChanged" class="text-sm text-muted">
          They'll be signed out and log in with the new email.
        </p>
        <UFormField
          v-if="!user"
          label="Password"
          name="password"
          hint="At least 10 characters"
          :error="errors.password"
        >
          <UInput
            v-model="password"
            name="password"
            type="password"
            autocomplete="new-password"
            class="w-full"
          />
        </UFormField>
        <label v-if="!isSelf" class="flex items-center gap-2 text-sm text-default">
          <input
            v-model="isAdmin"
            type="checkbox"
            name="is_admin"
            :class="checkboxClass"
          />
          Panel admin
        </label>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="user-form" :loading="pending">
          {{ user ? 'Save changes' : 'Create user' }}
        </UButton>
      </div>
    </template>
  </UModal>
</template>
