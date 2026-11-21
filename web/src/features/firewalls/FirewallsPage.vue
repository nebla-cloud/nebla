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
import { appliedServerCount, ruleCounts, useFirewalls } from './queries'

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
        <span class="flex min-w-0 flex-col gap-1">
          <span class="truncate">{{ f.name }}</span>
          <span class="whitespace-normal text-sm font-normal text-muted">
            {{ ruleCounts(f).inbound }} inbound · {{ ruleCounts(f).outbound }} outbound ·
            Applied to {{ plural(appliedServerCount(f), 'server') }}
          </span>
          <span v-if="Object.keys(f.labels).length" class="hidden flex-wrap gap-1 sm:flex">
            <LabelChip v-for="(v, k) in f.labels" :key="k" :k="String(k)" :v="v" />
          </span>
        </span>
      </ListRow>
    </ListGroup>

    <CreateFirewallModal v-model:open="creating" />
  </div>
</template>
