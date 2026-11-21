<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import UModal from '@nuxt/ui/components/Modal.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import { useCurrentProject } from '@/lib/project'
import { parseLabels } from './labelsInput'
import { useCreateFirewall } from './queries'

const open = defineModel<boolean>('open', { default: false })

const router = useRouter()
const { projectId } = useCurrentProject()
const create = useCreateFirewall()
const name = ref('')
const labels = ref('')
const nameError = ref<string | null>(null)
const labelsError = ref<string | null>(null)

watch(open, (v) => {
  if (v) return
  name.value = ''
  labels.value = ''
  nameError.value = null
  labelsError.value = null
})

async function submit() {
  nameError.value = null
  labelsError.value = null
  const n = name.value.trim()
  if (!n) {
    nameError.value = 'Give the firewall a name.'
    return
  }
  const parsed = parseLabels(labels.value)
  if ('error' in parsed) {
    labelsError.value = parsed.error
    return
  }
  try {
    const res = await create.mutateAsync({ name: n, labels: parsed.labels })
    open.value = false
    await router.push({
      name: 'firewall',
      params: { projectId: projectId.value, firewallId: res.firewall.id },
    })
  } catch (e) {
    nameError.value = errorMessage(e)
  }
}
</script>

<template>
  <UModal v-model:open="open" title="Create firewall" description="Add rules once it exists.">
    <template #body>
      <form id="create-firewall" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UFormField label="Name" name="name" :error="nameError ?? undefined">
          <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
        </UFormField>
        <UFormField
          label="Labels"
          name="labels"
          hint="Optional"
          help="Comma-separated, like tier=web, env=prod."
          :error="labelsError ?? undefined"
        >
          <UInput v-model="labels" name="labels" autocomplete="off" class="w-full font-mono" />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="create-firewall" :loading="create.isPending.value">
          Create firewall
        </UButton>
      </div>
    </template>
  </UModal>
</template>
