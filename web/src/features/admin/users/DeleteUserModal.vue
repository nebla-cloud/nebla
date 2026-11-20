<script setup lang="ts">
import { ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import { useDeleteUser, type AdminUser } from './queries'

const props = defineProps<{ user: AdminUser | null }>()
const open = defineModel<boolean>('open', { default: false })

const toast = useToast()
const del = useDeleteUser()
const error = ref<string | null>(null)
const soleOwner = ref(false)

watch(open, (v) => {
  if (!v) return
  error.value = null
  soleOwner.value = false
})

async function remove(takeOver: boolean) {
  if (!props.user) return
  error.value = null
  try {
    await del.mutateAsync({ id: props.user.id, takeOver })
    toast.add({ title: `Deleted ${props.user.email}`, color: 'success' })
    open.value = false
  } catch (e) {
    error.value = errorMessage(e)
    soleOwner.value = e instanceof ApiError && e.code === 'sole_owner'
  }
}
</script>

<template>
  <ConfirmByName
    v-model:open="open"
    :name="user?.email ?? ''"
    title="Delete this user?"
    :confirm-label="soleOwner ? 'Make me the owner and delete' : 'Delete user'"
    :loading="del.isPending.value"
    :error="error"
    @confirm="remove(soleOwner)"
  />
</template>
