<script setup lang="ts">
import { computed } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { errorMessage } from '@/api/errors'
import { useAdminSettings, useSaveSettings } from './queries'

const toast = useToast()
const { data: settings, isPending, error: loadError } = useAdminSettings()
const save = useSaveSettings()

// Follows the pending value while saving, so a failed save snaps back.
const signup = computed(() =>
  save.isPending.value ? save.variables.value!.self_signup : (settings.value?.self_signup ?? false),
)

async function toggle(on: boolean) {
  try {
    await save.mutateAsync({ self_signup: on })
    toast.add({ title: on ? 'Sign-ups are on' : 'Sign-ups are off', color: 'success' })
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="Settings" description="Panel-wide options that apply to everyone." />

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading settings…</p>
    <Panel v-else title="Sign-ups">
      <USwitch
        :model-value="signup"
        :disabled="save.isPending.value"
        label="Let anyone create an account"
        description="When off, people join only through an invite from a project owner or an account you create."
        @update:model-value="toggle"
      />
    </Panel>
  </div>
</template>
