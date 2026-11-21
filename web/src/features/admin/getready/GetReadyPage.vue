<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import PageHeader from '@/components/PageHeader.vue'
import Step from '@/components/Step.vue'
import StepList from '@/components/StepList.vue'
import { errorMessage } from '@/api/errors'
import { checklistSteps, useChecklistData, type StepKey } from './checklist'

const { data, error, retry } = useChecklistData()

const copy: Record<StepKey, { title: string; to: string; action: string }> = {
  location: { title: 'Connect Proxmox', to: 'admin-locations', action: 'Connect' },
  images: { title: 'Turn on OS images', to: 'admin-catalog', action: 'Choose images' },
  types: { title: 'Add server types', to: 'admin-server-types', action: 'Add' },
  pools: { title: 'Add an IP pool', to: 'admin-ip-pools', action: 'Add' },
}

const steps = computed(() =>
  (data.value ? checklistSteps(data.value) : []).map((s) => ({ ...s, ...copy[s.key] })),
)
const firstOpen = computed(() => steps.value.findIndex((s) => !s.done))
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <PageHeader
      title="Get Nebla ready"
      description="Four steps, then anyone in a project can create servers."
    />
    <div v-if="error" class="flex items-center gap-3 text-sm" role="alert">
      <span class="text-error">{{ errorMessage(error) }}</span>
      <UButton size="sm" color="neutral" variant="outline" @click="retry">Try again</UButton>
    </div>
    <p v-else-if="!data" class="text-sm text-muted">Loading your setup</p>
    <StepList v-else>
      <Step v-for="(s, i) in steps" :key="s.key" :title="s.title" :done="s.done">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="min-w-0 text-sm text-muted">{{ s.detail }}</p>
          <UButton
            :to="{ name: s.to }"
            size="sm"
            :color="i === firstOpen ? 'primary' : 'neutral'"
            :variant="i === firstOpen ? 'solid' : s.done ? 'ghost' : 'outline'"
          >
            {{ s.done ? 'Manage' : s.action }}
          </UButton>
        </div>
      </Step>
    </StepList>
  </div>
</template>
