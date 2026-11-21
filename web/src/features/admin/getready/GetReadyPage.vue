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
