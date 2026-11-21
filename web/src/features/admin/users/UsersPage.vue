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
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import AdminPager from '@/components/AdminPager.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useSession } from '@/lib/session'
import { table, tableWrap, tbody, td, th, thead, tr } from '@/lib/ui'
import { usePageClamp } from '@/lib/usePageClamp'
import DeleteUserModal from './DeleteUserModal.vue'
import ResetPasswordModal from './ResetPasswordModal.vue'
import UserFormModal from './UserFormModal.vue'
import {
  PER_PAGE,
  useAdminUsers,
  useDisableTotp,
  useRemovePasskeys,
  type AdminUser,
} from './queries'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const { me } = useSession()

const page = computed(() => Math.max(1, Number.parseInt(String(route.query.page ?? '1'), 10) || 1))
const { data, isPending, error: loadError } = useAdminUsers(page)
const users = computed(() => data.value?.users ?? [])
const total = computed(() => data.value?.meta.pagination.total_entries ?? 0)
usePageClamp(
  page,
  computed(() => data.value?.users.length),
  total,
  PER_PAGE,
)

const disableTotp = useDisableTotp()
const removePasskeys = useRemovePasskeys()

const formOpen = ref(false)
const editing = ref<AdminUser | undefined>()
const resetOpen = ref(false)
const totpOpen = ref(false)
const passkeysOpen = ref(false)
const deleteOpen = ref(false)
const target = ref<AdminUser | null>(null)

const label = (u: AdminUser) => u.name || u.email
const isSelf = (u: AdminUser) => u.id === me.value?.id

function openForm(u?: AdminUser) {
  editing.value = u
  formOpen.value = true
}

function act(u: AdminUser, open: { value: boolean }) {
  target.value = u
  open.value = true
}

function menu(u: AdminUser) {
  const items = [{ label: 'Edit', icon: 'i-lucide-pencil', onSelect: () => openForm(u) }]
  if (isSelf(u)) return [items]
  items.push({
    label: 'Reset password',
    icon: 'i-lucide-key-round',
    onSelect: () => act(u, resetOpen),
  })
  if (u.totp_enabled) {
    items.push({
      label: 'Turn off two-factor',
      icon: 'i-lucide-shield-off',
      onSelect: () => act(u, totpOpen),
    })
  }
  if (u.passkey_count > 0) {
    items.push({
      label: 'Remove passkeys',
      icon: 'i-lucide-fingerprint',
      onSelect: () => act(u, passkeysOpen),
    })
  }
  return [
    items,
    [
      {
        label: 'Delete',
        icon: 'i-lucide-trash-2',
        color: 'error',
        onSelect: () => act(u, deleteOpen),
      },
    ],
  ]
}

function setPage(p: number) {
  void router.push({ query: { ...route.query, page: String(p) } })
}

async function run(
  fn: (id: number) => Promise<unknown>,
  close: () => void,
  done: (u: AdminUser) => string,
) {
  const u = target.value
  if (!u) return
  try {
    await fn(u.id)
    toast.add({ title: done(u), color: 'success' })
    close()
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader
      title="Users"
      description="There's no email from here. Send them their password yourself."
    >
      <template #actions>
        <UButton icon="i-lucide-user-plus" @click="openForm()">New user</UButton>
      </template>
    </PageHeader>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading users…</p>
    <EmptyState v-else-if="!users.length" title="No users" body="Nobody has an account yet." />
    <div v-else :class="tableWrap">
      <table :class="table">
        <thead :class="thead">
          <tr>
            <th scope="col" :class="th">Name</th>
            <th scope="col" :class="th">Email</th>
            <th scope="col" :class="th">Admin</th>
            <th scope="col" :class="th">Two-factor</th>
            <th scope="col" :class="th">Passkeys</th>
            <th scope="col" :class="th"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody :class="tbody">
          <tr v-for="u in users" :key="u.id" :class="[tr, 'max-sm:relative max-sm:pr-14']">
            <td :class="[td, 'min-w-0 wrap-anywhere font-semibold text-highlighted']">
              {{ label(u) }}
              <span v-if="isSelf(u)" class="font-normal text-muted">(you)</span>
            </td>
            <td :class="[td, 'order-last basis-full truncate text-muted']">
              {{ u.email }}
            </td>
            <td :class="td">
              <UBadge v-if="u.is_admin" color="primary" variant="subtle">Admin</UBadge>
            </td>
            <td :class="[td, 'text-muted']">
              <span class="sm:hidden">Two-factor </span>{{ u.totp_enabled ? 'On' : 'Off' }}
            </td>
            <td data-col="passkeys" :class="[td, 'text-muted']">
              <template v-if="u.passkey_count > 0">
                <span class="sm:hidden">Passkeys </span>{{ u.passkey_count }}
              </template>
            </td>
            <td data-col="actions" :class="[td, 'max-sm:absolute max-sm:top-2.5 max-sm:right-3 sm:text-right']">
              <UDropdownMenu :items="menu(u)" :content="{ align: 'end' }">
                <UButton
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-ellipsis"
                  :aria-label="`Actions for ${label(u)}`"
                />
              </UDropdownMenu>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <AdminPager :page="page" :per-page="PER_PAGE" :total="total" @update:page="setPage" />

    <UserFormModal v-model:open="formOpen" :user="editing" />
    <ResetPasswordModal v-model:open="resetOpen" :user="target" />
    <DeleteUserModal v-model:open="deleteOpen" :user="target" />
    <ConfirmDialog
      v-model:open="totpOpen"
      :title="`Turn off two-factor for ${target ? label(target) : ''}?`"
      body="They'll log in with just their password and can turn it back on from their account page."
      confirm-label="Turn off two-factor"
      danger
      :loading="disableTotp.isPending.value"
      @confirm="
        run(
          (id) => disableTotp.mutateAsync(id),
          () => (totpOpen = false),
          (u) => `Two-factor is off for ${label(u)}`,
        )
      "
    />
    <ConfirmDialog
      v-model:open="passkeysOpen"
      :title="`Remove passkeys for ${target ? label(target) : ''}?`"
      body="They'll sign in with their password (and two-factor code, if it's on) until they add a new passkey."
      confirm-label="Remove passkeys"
      danger
      :loading="removePasskeys.isPending.value"
      @confirm="
        run(
          (id) => removePasskeys.mutateAsync(id),
          () => (passkeysOpen = false),
          (u) => `Removed passkeys for ${label(u)}`,
        )
      "
    />
  </div>
</template>
