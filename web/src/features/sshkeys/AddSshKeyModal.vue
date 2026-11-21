<script setup lang="ts">
import { ref, watch } from 'vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { errorMessage } from '@/api/errors'
import { useCreateSshKey, type SshKey } from './queries'

const emit = defineEmits<{ created: [key: SshKey] }>()
const open = defineModel<boolean>('open', { default: false })

const PUBLIC_KEY =
  /^(ssh-(ed25519|rsa)|ecdsa-sha2-nistp(256|384|521)|sk-[a-z0-9@.-]+) [A-Za-z0-9+/=]+( .*)?$/

const create = useCreateSshKey()
const name = ref('')
const publicKey = ref('')
const keyError = ref<string | null>(null)
const formError = ref<string | null>(null)

watch(open, (v) => {
  if (v) return
  name.value = ''
  publicKey.value = ''
  keyError.value = null
  formError.value = null
})

function commentOf(key: string): string {
  const parts = key.split(' ')
  return parts.slice(2).join(' ').trim()
}

async function submit() {
  keyError.value = null
  formError.value = null
  const key = publicKey.value.trim()
  if (!PUBLIC_KEY.test(key)) {
    keyError.value =
      "That doesn't look like an OpenSSH public key. It starts with ssh-ed25519 or ssh-rsa."
    return
  }
  const finalName = name.value.trim() || commentOf(key)
  if (!finalName) {
    formError.value = 'Give the key a name.'
    return
  }
  try {
    const created = await create.mutateAsync({ name: finalName, public_key: key })
    emit('created', created)
    open.value = false
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <UModal v-model:open="open" title="Add SSH key" description="Paste the public half of your key.">
    <template #body>
      <form id="add-ssh-key" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UFormField label="Public key" name="public_key" :error="keyError ?? undefined">
          <UTextarea
            v-model="publicKey"
            name="public_key"
            :rows="4"
            spellcheck="false"
            placeholder="ssh-ed25519 AAAA… you@laptop"
            class="w-full nb-mono"
          />
        </UFormField>
        <UFormField
          label="Name"
          name="name"
          hint="Optional"
          help="Defaults to the comment at the end of the key."
          :error="formError ?? undefined"
        >
          <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="add-ssh-key" :loading="create.isPending.value">
          Add key
        </UButton>
      </div>
    </template>
  </UModal>
</template>
