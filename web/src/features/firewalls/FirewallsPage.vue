<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import { errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import LabelChip from '@/components/LabelChip.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useCurrentProject } from '@/lib/project'
import { canWrite } from '@/lib/roles'
import CreateFirewallModal from './CreateFirewallModal.vue'
import { ruleCounts, useFirewalls } from './queries'

const { projectId, project } = useCurrentProject()
const writable = computed(() => canWrite(project.value?.role))
const { data: firewalls, isPending, error } = useFirewalls()
const creating = ref(false)

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="Firewalls" description="Rules for the traffic your servers accept and send.">
      <template v-if="writable && firewalls?.length" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create firewall</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending" class="text-muted">Loading firewalls…</p>
    <EmptyState
      v-else-if="!firewalls?.length"
      title="No firewalls yet"
      body="A firewall holds Inbound and Outbound rules. Apply it to servers directly or by label."
    >
      <UButton v-if="writable" icon="i-lucide-plus" @click="creating = true">
        Create firewall
      </UButton>
    </EmptyState>
    <ListGroup v-else label="Firewalls">
      <ListRow
        v-for="f in firewalls"
        :key="f.id"
        :to="{ name: 'firewall', params: { projectId, firewallId: f.id } }"
      >
        <template #leading><UIcon name="i-lucide-shield" class="size-4 text-muted" /></template>
        {{ f.name }}
        <template #meta>
          {{ ruleCounts(f).inbound }} inbound · {{ ruleCounts(f).outbound }} outbound ·
          Applied to {{ plural(f.applied_to.length, 'target') }}
        </template>
        <template v-if="Object.keys(f.labels).length" #trailing>
          <span class="hidden gap-1 sm:flex">
            <LabelChip v-for="(v, k) in f.labels" :key="k" :k="String(k)" :v="v" />
          </span>
        </template>
      </ListRow>
    </ListGroup>

    <CreateFirewallModal v-model:open="creating" />
  </div>
</template>
