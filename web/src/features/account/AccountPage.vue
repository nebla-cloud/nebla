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
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import AccountMenu from '@/components/AccountMenu.vue'
import Lockup from '@/components/Lockup.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { projectQuery } from '@/features/projects/queries'
import TopBar from '@/layouts/TopBar.vue'
import { readLastProject } from '@/lib/project'
import { setMe, useSession } from '@/lib/session'
import { inlineError } from './authError'
import PasskeysSection from './PasskeysSection.vue'
import TwoFactorSection from './TwoFactorSection.vue'

const toast = useToast()
const { me } = useSession()

const lastId = readLastProject()
const { data: lastProject } = useQuery(
  computed(() => ({ ...projectQuery(lastId ?? 0), enabled: lastId !== null })),
)

const name = ref(me.value?.name ?? '')
watch(
  () => me.value?.name,
  (n) => {
    if (n !== undefined) name.value = n
  },
)
const profileBusy = ref(false)
const profileError = ref<string | null>(null)

async function saveName() {
  if (profileBusy.value) return
  profileBusy.value = true
  profileError.value = null
  try {
    setMe(unwrap(await api.PUT('/me', { body: { name: name.value.trim() } })))
    toast.add({ title: 'Name saved', color: 'success' })
  } catch (e) {
    profileError.value = inlineError(e)
  } finally {
    profileBusy.value = false
  }
}

const pw = reactive({ current: '', next: '', confirm: '' })
const pwBusy = ref(false)
const pwError = ref<string | null>(null)

async function changePassword() {
  if (pwBusy.value) return
  pwError.value = null
  if (pw.next !== pw.confirm) {
    pwError.value = "Passwords don't match."
    return
  }
  pwBusy.value = true
  try {
    unwrap(
      await api.POST('/me/password', {
        body: { current_password: pw.current, new_password: pw.next },
      }),
    )
    pw.current = pw.next = pw.confirm = ''
    toast.add({
      title: 'Password changed. Your other sessions were signed out.',
      color: 'success',
    })
  } catch (e) {
    pwError.value = inlineError(e)
  } finally {
    pwBusy.value = false
  }
}
</script>

<template>
  <div class="min-h-dvh">
    <TopBar>
      <template #leading>
        <span class="flex min-w-0 px-2 lg:px-0"><Lockup :height="22" /></span>
      </template>
      <template #actions>
        <UButton v-if="me?.is_admin" to="/admin" color="neutral" variant="ghost">Admin</UButton>
        <div class="w-44 sm:w-56"><AccountMenu /></div>
      </template>
    </TopBar>

    <main class="mx-auto flex w-full max-w-2xl flex-col gap-4 px-4 py-10 sm:px-8">
      <RouterLink
        v-if="lastProject"
        :to="`/p/${lastProject.id}/servers`"
        class="inline-flex w-fit items-center gap-1.5 text-sm text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
      >
        <UIcon name="i-lucide-arrow-left" class="size-4" />
        Back to {{ lastProject.name }}
      </RouterLink>
      <RouterLink
        v-else
        to="/projects"
        class="inline-flex w-fit items-center gap-1.5 text-sm text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
      >
        <UIcon name="i-lucide-arrow-left" class="size-4" />
        Go to projects
      </RouterLink>

      <PageHeader title="Account settings" />

      <Panel title="Profile">
        <div class="flex flex-col gap-3">
          <UAlert v-if="profileError" color="error" variant="subtle" :description="profileError" />
          <form class="flex flex-col gap-4" novalidate @submit.prevent="saveName">
            <UFormField label="Name" name="name">
              <UInput v-model="name" name="name" autocomplete="name" class="w-full" />
            </UFormField>
            <div>
              <p class="text-sm font-medium text-highlighted">Email</p>
              <p class="text-sm text-toned">{{ me?.email }}</p>
            </div>
            <div>
              <UButton type="submit" :loading="profileBusy">Save</UButton>
            </div>
          </form>
        </div>
      </Panel>

      <Panel title="Password">
        <div class="flex flex-col gap-3">
          <UAlert v-if="pwError" color="error" variant="subtle" :description="pwError" />
          <form class="flex flex-col gap-4" novalidate @submit.prevent="changePassword">
            <UFormField label="Current password" name="current_password">
              <UInput
                v-model="pw.current"
                name="current_password"
                type="password"
                autocomplete="current-password"
                class="w-full"
              />
            </UFormField>
            <UFormField label="New password" name="new_password">
              <UInput
                v-model="pw.next"
                name="new_password"
                type="password"
                autocomplete="new-password"
                class="w-full"
              />
            </UFormField>
            <UFormField label="Confirm new password" name="confirm_password">
              <UInput
                v-model="pw.confirm"
                name="confirm_password"
                type="password"
                autocomplete="new-password"
                class="w-full"
              />
            </UFormField>
            <div>
              <UButton type="submit" :loading="pwBusy">Change password</UButton>
            </div>
          </form>
        </div>
      </Panel>

      <TwoFactorSection />
      <PasskeysSection />
    </main>
  </div>
</template>
