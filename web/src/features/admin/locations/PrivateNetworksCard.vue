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
import { ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { AdminLocation } from '@/api/types'
import Panel from '@/components/Panel.vue'
import TerminalBlock from '@/components/TerminalBlock.vue'
import { useSaveLocation, type LocationInput } from './queries'

const AUDIT = 'pveum role modify Nebla --append 1 --privs SDN.Audit'
const PREFIX = /^[a-z][a-z0-9]{0,3}$/

const props = defineProps<{ location: AdminLocation }>()

const toast = useToast()
const save = useSaveLocation(props.location.id)
const zone = ref(props.location.sdn_zone ?? '')
const prefix = ref(props.location.vnet_prefix)
const prefixError = ref<string | null>(null)
const formError = ref<string | null>(null)

watch(
  () => [props.location.sdn_zone, props.location.vnet_prefix] as const,
  ([z, p]) => {
    zone.value = z ?? ''
    prefix.value = p
  },
)

async function submit() {
  prefixError.value = formError.value = null
  const p = prefix.value.trim()
  if (!PREFIX.test(p)) {
    prefixError.value = 'Use 1 to 4 lowercase letters or digits, starting with a letter.'
    return
  }
  const l = props.location
  const body: LocationInput = {
    name: l.name,
    description: l.description,
    pve: {
      url: l.pve.url,
      token_id: l.pve.token_id,
      ca_fingerprint: l.pve.ca_fingerprint,
      insecure_tls: l.pve.insecure_tls,
    },
    disk_storage: l.disk_storage,
    iso_storage: l.iso_storage,
    import_storage: l.import_storage,
    sdn_zone: zone.value.trim(),
    vnet_prefix: p,
  }
  try {
    await save.mutateAsync(body)
    toast.add({ title: 'Private networks saved', color: 'success' })
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <Panel title="Private networks" data-testid="private-networks">
    <div class="flex flex-col gap-4">
      <p v-if="location.private_networks.state === 'not_set_up'" class="text-sm text-muted">
        Private networks are off at this location. Enter the name of the SDN zone that holds the
        VNets Nebla may use to turn them on.
      </p>

      <UAlert
        v-else-if="location.private_networks.state === 'needs_setup'"
        color="warning"
        variant="subtle"
        title="Nebla can't see the zone yet"
      >
        <template #description>
          <p>The API token needs to read SDN. Run this on the Proxmox node:</p>
          <TerminalBlock as="pre" class="mt-2">{{ AUDIT }}</TerminalBlock>
        </template>
      </UAlert>

      <UAlert
        v-else-if="location.private_networks.state === 'firewall_off'"
        color="warning"
        variant="subtle"
        description="Isolation off: turn the datacenter firewall back on."
      />

      <p v-if="location.private_networks.zone" class="text-sm text-muted tabular-nums">
        {{ location.private_networks.available }} available,
        {{ location.private_networks.in_use }} in use,
        {{ location.private_networks.cooling }} cooling
      </p>

      <div v-if="location.private_networks.unusable.length">
        <h3 class="mb-1 text-sm font-semibold text-highlighted">Unusable VNets</h3>
        <ul class="text-sm text-muted">
          <li v-for="u in location.private_networks.unusable" :key="u.vnet">
            <span class="nb-mono text-default">{{ u.vnet }}</span
            >: {{ u.reason }}
          </li>
        </ul>
      </div>

      <div v-if="location.private_networks.degraded.length">
        <h3 class="mb-1 text-sm font-semibold text-highlighted">Degraded VNets</h3>
        <ul class="text-sm text-muted">
          <li v-for="d in location.private_networks.degraded" :key="d.vnet">
            <span class="nb-mono text-default">{{ d.vnet }}</span> (network {{ d.network }}):
            {{ d.reason }}
          </li>
        </ul>
      </div>

      <form
        id="private-networks"
        class="flex max-w-md flex-col gap-4"
        novalidate
        @submit.prevent="submit"
      >
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <UFormField
          label="SDN zone"
          name="sdn_zone"
          help="Leave empty to turn private networks off. Can't change while networks are in use."
        >
          <UInput
            v-model="zone"
            name="sdn_zone"
            autocomplete="off"
            spellcheck="false"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="VNet prefix"
          name="vnet_prefix"
          help="Nebla only uses VNets whose names start with this."
          :error="prefixError ?? undefined"
        >
          <UInput
            v-model="prefix"
            name="vnet_prefix"
            autocomplete="off"
            spellcheck="false"
            class="w-full"
          />
        </UFormField>
        <div>
          <UButton type="submit" :loading="save.isPending.value">Save</UButton>
        </div>
      </form>
    </div>
  </Panel>
</template>
