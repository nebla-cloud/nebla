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
