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
import { ruleCounts, useFirewalls } from './queries'

const model = defineModel<number[]>({ default: () => [] })
const { data: firewalls } = useFirewalls()
const picked = computed({
  get: () => model.value.map(String),
  set: (v: string[]) => (model.value = v.map(Number)),
})
</script>

<template>
  <Step v-if="firewalls?.length" title="Firewalls" :done="model.length > 0">
    <fieldset>
      <legend class="sr-only">Firewalls</legend>
      <div class="grid gap-2 sm:grid-cols-2">
        <ChoiceCard
          v-for="f in firewalls"
          :key="f.id"
          v-model="picked"
          type="checkbox"
          name="firewall"
          :value="String(f.id)"
        >
          {{ f.name }}
          <template #meta>
            {{ ruleCounts(f).inbound }} inbound · {{ ruleCounts(f).outbound }} outbound
          </template>
        </ChoiceCard>
      </div>
    </fieldset>
  </Step>
</template>
