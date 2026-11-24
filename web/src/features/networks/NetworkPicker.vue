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
import ChoiceCard from '@/components/ChoiceCard.vue'
import Step from '@/components/Step.vue'
import type { Location } from '@/api/types'
import { MAX_PER_SERVER } from './choices'
import { useNetworks } from './queries'

const props = defineProps<{ location?: Location }>()
const model = defineModel<number[]>({ default: () => [] })

const { data: networks } = useNetworks()
const here = computed(() =>
  props.location?.private_networks_available
    ? (networks.value ?? []).filter((n) => n.location.id === props.location!.id)
    : [],
)
const picked = computed({
  get: () => model.value.map(String),
  set: (v: string[]) => (model.value = v.map(Number)),
})
const full = computed(() => model.value.length >= MAX_PER_SERVER)
</script>

<template>
  <Step v-if="here.length" title="Private networks" :done="model.length > 0">
    <fieldset>
      <legend class="sr-only">Private networks</legend>
      <div class="grid gap-2 sm:grid-cols-2">
        <ChoiceCard
          v-for="n in here"
          :key="n.id"
          v-model="picked"
          type="checkbox"
          name="network"
          :value="String(n.id)"
          :disabled="full && !model.includes(n.id)"
        >
          {{ n.name }}
          <template #meta>{{ n.ip_range }}</template>
        </ChoiceCard>
      </div>
      <p class="mt-2 text-xs text-muted">Up to {{ MAX_PER_SERVER }} networks per server.</p>
    </fieldset>
  </Step>
</template>
