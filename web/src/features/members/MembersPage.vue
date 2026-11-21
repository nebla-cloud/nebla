<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UAvatar from '@nuxt/ui/components/Avatar.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Invite, Member, Role } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import ListGroup from '@/components/ListGroup.vue'
import PageHeader from '@/components/PageHeader.vue'
import { selectClass } from '@/lib/ui'
import { useCurrentProject } from '@/lib/project'
import { assignableRoles, isManager, roleLabel } from '@/lib/roles'
import { useSession } from '@/lib/session'
import InviteModal from './InviteModal.vue'
import { useInvites, useMembers, useRemoveMember, useRevokeInvite, useSetRole } from './queries'

const TOKEN_NOTE = 'API tokens they made in this project are revoked.'
const RANK: Record<Role, number> = { owner: 3, admin: 2, member: 1, read_only: 0 }

const toast = useToast()
const { me } = useSession()
const myId = computed(() => me.value?.id)
const { projectId, project } = useCurrentProject()
const manager = computed(() => isManager(project.value?.role))
const myRole = computed(() => project.value?.role)

const { data: members, isPending, error: loadError } = useMembers(projectId)
const { data: invites } = useInvites(projectId, manager)
const setRole = useSetRole(projectId, myId)
const removeMember = useRemoveMember(projectId, myId)
const revoke = useRevokeInvite(projectId)

const inviting = ref(false)
const pendingRole = ref<{ member: Member; role: Role } | null>(null)
const roleOpen = ref(false)
const removing = ref<Member | null>(null)
const removeOpen = ref(false)
const revoking = ref<Invite | null>(null)
const revokeOpen = ref(false)

const roleOptions = computed(() => (myRole.value ? assignableRoles(myRole.value) : []))

function canEdit(m: Member): boolean {
  if (!manager.value) return false
  return myRole.value === 'owner' || m.role !== 'owner'
}

function expiresIn(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (ms < 86_400_000) return 'expires today'
  const days = Math.ceil(ms / 86_400_000)
  return `expires in ${days} ${days === 1 ? 'day' : 'days'}`
}

async function applyRole(m: Member, role: Role, el?: HTMLSelectElement) {
  try {
    await setRole.mutateAsync({ userId: m.user_id, role })
  } catch (e) {
    if (el) el.value = m.role
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}

function onRoleChange(m: Member, ev: Event) {
  const el = ev.target as HTMLSelectElement
  const role = el.value as Role
  if (role === m.role) return
  const lower = RANK[role] < RANK[m.role]
  if (lower && (m.user_id === myId.value || role === 'member' || role === 'read_only')) {
    el.value = m.role
    pendingRole.value = { member: m, role }
    roleOpen.value = true
    return
  }
  void applyRole(m, role, el)
}

async function confirmRole() {
  const p = pendingRole.value
  if (!p) return
  await applyRole(p.member, p.role)
  roleOpen.value = false
}

const roleBody = computed(() => {
  const m = pendingRole.value?.member
  const keeps = isManager(pendingRole.value?.role)
  if (m && m.user_id === myId.value)
    return `You'll lose some access. API tokens you made ${keeps ? 'keep working' : 'in this project are revoked'}.`
  const who = m?.name || m?.email || 'They'
  return `${who} will lose some access. ${keeps ? 'API tokens they made keep working.' : TOKEN_NOTE}`
})

function startRemove(m: Member) {
  removing.value = m
  removeOpen.value = true
}

async function confirmRemove() {
  const m = removing.value
  if (!m) return
  try {
    await removeMember.mutateAsync(m.user_id)
    toast.add({ title: `Removed ${m.name || m.email}`, color: 'success' })
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
  removeOpen.value = false
}

function startRevoke(i: Invite) {
  revoking.value = i
  revokeOpen.value = true
}

async function confirmRevoke() {
  const i = revoking.value
  if (!i) return
  try {
    await revoke.mutateAsync(i.id)
    toast.add({ title: `Revoked the invite for ${i.email}`, color: 'success' })
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
  revokeOpen.value = false
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="Members">
      <template v-if="manager" #actions>
        <UButton icon="i-lucide-user-plus" @click="inviting = true">Invite</UButton>
      </template>
    </PageHeader>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading members…</p>
    <ListGroup v-else>
      <li
        v-for="m in members"
        :key="m.user_id"
        class="flex flex-col gap-2 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="flex min-w-0 items-center gap-3">
          <UAvatar :alt="m.name || m.email" size="md" />
          <div class="flex min-w-0 flex-col">
            <span class="truncate font-semibold text-highlighted">
              {{ m.name || m.email }}
              <span v-if="m.user_id === myId" class="font-normal text-muted">(you)</span>
            </span>
            <span class="truncate text-sm text-muted">{{ m.email }}</span>
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <template v-if="canEdit(m)">
            <select
              :value="m.role"
              :aria-label="`Role for ${m.name || m.email}`"
              :class="selectClass"
              @change="onRoleChange(m, $event)"
            >
              <option v-for="r in roleOptions" :key="r" :value="r">{{ roleLabel(r) }}</option>
            </select>
            <UButton
              v-if="m.user_id !== myId"
              color="error"
              variant="ghost"
              size="sm"
              icon="i-lucide-user-minus"
              :aria-label="`Remove ${m.name || m.email}`"
              @click="startRemove(m)"
            >
              Remove
            </UButton>
          </template>
          <span v-else class="text-sm text-muted">{{ roleLabel(m.role) }}</span>
        </div>
      </li>
    </ListGroup>

    <section v-if="manager && invites?.length" class="flex flex-col gap-3">
      <h2 class="nb-h2 text-highlighted">Pending invites</h2>
      <ListGroup>
        <li
          v-for="i in invites"
          :key="i.id"
          class="flex flex-col gap-2 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
        >
          <div class="flex min-w-0 flex-col">
            <span class="truncate font-semibold text-highlighted">{{ i.email }}</span>
            <span class="text-sm text-muted"
              >{{ roleLabel(i.role) }}, {{ expiresIn(i.expires) }}</span
            >
          </div>
          <UButton
            color="error"
            variant="ghost"
            size="sm"
            icon="i-lucide-x"
            class="self-start sm:self-auto"
            :aria-label="`Revoke invite for ${i.email}`"
            @click="startRevoke(i)"
          >
            Revoke
          </UButton>
        </li>
      </ListGroup>
    </section>

    <InviteModal v-if="manager" v-model:open="inviting" />

    <ConfirmDialog
      v-model:open="removeOpen"
      :title="`Remove ${removing?.name || removing?.email || 'member'}?`"
      :body="`They lose access to this project. ${TOKEN_NOTE}`"
      confirm-label="Remove"
      danger
      :loading="removeMember.isPending.value"
      @confirm="confirmRemove"
    />
    <ConfirmDialog
      v-model:open="roleOpen"
      :title="`Change role to ${pendingRole ? roleLabel(pendingRole.role) : ''}?`"
      :body="roleBody"
      confirm-label="Change role"
      danger
      :loading="setRole.isPending.value"
      @confirm="confirmRole"
    />
    <ConfirmDialog
      v-model:open="revokeOpen"
      :title="`Revoke invite for ${revoking?.email ?? ''}?`"
      body="The link stops working."
      confirm-label="Revoke"
      danger
      :loading="revoke.isPending.value"
      @confirm="confirmRevoke"
    />
  </div>
</template>
