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
import { computed, reactive, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { errorMessage } from '@/api/errors'
import type { AdminLocation } from '@/api/types'
import { useSaveLocation, type LocationInput } from './queries'

const props = defineProps<{ location?: AdminLocation; backupStorages?: string[] }>()
const emit = defineEmits<{ saved: [AdminLocation]; cancel: [] }>()

// A literal "//" in a template attribute breaks vue-tsc here.
const urlExample = ['https:', 'pve.example.com:8006'].join('//')
const editing = computed(() => props.location !== undefined)
const saveLocation = useSaveLocation(props.location?.id)

const form = reactive({
  name: props.location?.name ?? '',
  description: props.location?.description ?? '',
  url: props.location?.pve.url ?? '',
  token_id: props.location?.pve.token_id ?? '',
  token_secret: '',
  ca_fingerprint: props.location?.pve.ca_fingerprint ?? '',
  insecure_tls: props.location?.pve.insecure_tls ?? false,
  disk_storage: props.location?.disk_storage ?? '',
  iso_storage: props.location?.iso_storage ?? '',
  import_storage: props.location?.import_storage ?? '',
  volume_storage: props.location?.volume_storage ?? '',
  backup_storage: props.location?.backup_storage ?? '',
  window_start: props.location?.backup_window.start ?? '02:00',
  window_end: props.location?.backup_window.end ?? '06:00',
  snapshot_storage: props.location?.snapshot_storage ?? '',
})
// The reported PBS storages, plus the one in use even if the last report no longer lists it.
const pbsOptions = computed(() => [
  ...new Set([...(props.backupStorages ?? []), ...(props.location?.backup_storage ? [props.location.backup_storage] : [])]),
])
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!/^[a-z0-9-]{1,32}$/.test(form.name)) {
    e.name = 'Use 1 to 32 lowercase letters, numbers or hyphens.'
  }
  if (!form.url.trim()) e.url = 'Enter the Proxmox address.'
  if (!form.token_id.trim()) e.token_id = 'Enter the API token ID.'
  if (!editing.value && !form.token_secret) e.token_secret = 'Enter the API token secret.'
  if (!form.disk_storage.trim()) e.disk_storage = 'Enter the disk storage name.'
  if (!form.iso_storage.trim()) e.iso_storage = 'Enter the ISO storage name.'
  if (editing.value && form.window_start === form.window_end)
    e.backup_window = 'The window must start and end at different times.'
  errors.value = e
  return Object.keys(e).length === 0
}

function body(): LocationInput {
  const edit = editing.value
  const fingerprint = form.ca_fingerprint.trim()
  const out: LocationInput = {
    name: form.name,
    pve: {
      url: form.url.trim(),
      token_id: form.token_id.trim(),
      insecure_tls: form.insecure_tls,
    },
    disk_storage: form.disk_storage.trim(),
    iso_storage: form.iso_storage.trim(),
  }
  if (form.token_secret) out.pve.token_secret = form.token_secret
  if (edit || fingerprint) out.pve.ca_fingerprint = fingerprint
  if (edit || form.description.trim()) out.description = form.description.trim()
  if (edit || form.import_storage.trim()) out.import_storage = form.import_storage.trim()
  if (edit || form.volume_storage.trim()) out.volume_storage = form.volume_storage.trim()
  if (edit) {
    out.backup_storage = form.backup_storage.trim()
    out.backup_window = { start: form.window_start, end: form.window_end }
  }
  if (edit || form.snapshot_storage.trim()) out.snapshot_storage = form.snapshot_storage.trim()
  return out
}

async function submit() {
  formError.value = null
  if (!validate()) return
  try {
    emit('saved', await saveLocation.mutateAsync(body()))
  } catch (e) {
    const message = errorMessage(e)
    if (message.startsWith('import_storage ')) {
      errors.value.import_storage = message
    } else if (message.startsWith('backup storage ')) {
      errors.value.backup_storage = message
    } else {
      formError.value = message
    }
  }
}
</script>

<template>
  <form class="flex max-w-2xl flex-col gap-5" novalidate @submit.prevent="submit">
    <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />

    <UFormField
      label="Name"
      name="name"
      help="Lowercase letters, numbers and hyphens, up to 32. People see this when they pick where to run a server."
      :error="errors.name"
    >
      <UInput v-model="form.name" name="name" autocomplete="off" class="w-full" />
    </UFormField>
    <UFormField label="Description" name="description" help="Optional. A line about where this is.">
      <UInput v-model="form.description" name="description" autocomplete="off" class="w-full" />
    </UFormField>

    <fieldset class="flex flex-col gap-5">
      <legend class="mb-3 font-bold tracking-tight text-highlighted">Proxmox connection</legend>
      <UFormField
        label="Proxmox URL"
        name="url"
        help="The address of the Proxmox API, usually on port 8006."
        :error="errors.url"
      >
        <UInput
          v-model="form.url"
          name="url"
          type="url"
          :placeholder="urlExample"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
      <UFormField
        label="API token ID"
        name="token_id"
        help="Create it under Datacenter › Permissions › API tokens. See docs/proxmox-setup.md for the role."
        :error="errors.token_id"
      >
        <UInput
          v-model="form.token_id"
          name="token_id"
          placeholder="nebla@pve!panel"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
      <UFormField
        label="API token secret"
        name="token_secret"
        :help="
          editing
            ? 'Leave blank to keep the stored secret.'
            : 'Proxmox shows the secret once, when you create the token.'
        "
        :error="errors.token_secret"
      >
        <UInput
          v-model="form.token_secret"
          name="token_secret"
          type="password"
          autocomplete="new-password"
          class="w-full"
        />
      </UFormField>
      <UFormField
        label="Certificate fingerprint"
        name="ca_fingerprint"
        help="For a self-signed certificate. Paste the SHA-256 fingerprint from Datacenter › Nodes › Certificates."
      >
        <UInput
          v-model="form.ca_fingerprint"
          name="ca_fingerprint"
          autocomplete="off"
          spellcheck="false"
          :disabled="form.insecure_tls"
          class="w-full"
        />
      </UFormField>
      <UCheckbox
        v-model="form.insecure_tls"
        name="insecure_tls"
        label="Skip certificate check"
        description="Trusts any certificate Proxmox presents. Use it only on a network you control."
        :disabled="form.ca_fingerprint.trim() !== ''"
      />
    </fieldset>

    <fieldset class="flex flex-col gap-5">
      <legend class="mb-3 font-bold tracking-tight text-highlighted">Storage</legend>
      <UFormField
        label="Disk storage"
        name="disk_storage"
        help="Where server disks live, for example local-lvm."
        :error="errors.disk_storage"
      >
        <UInput v-model="form.disk_storage" name="disk_storage" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField
        label="ISO storage"
        name="iso_storage"
        help="Where ISO images are kept, for example local."
        :error="errors.iso_storage"
      >
        <UInput v-model="form.iso_storage" name="iso_storage" autocomplete="off" class="w-full" />
      </UFormField>
      <UFormField
        label="Import storage"
        name="import_storage"
        help="Enable the Import content type on this storage in Proxmox before saving. Leave blank to disable catalog template builds."
        :error="errors.import_storage"
      >
        <UInput
          v-model="form.import_storage"
          name="import_storage"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
      <UFormField
        label="Volume storage"
        name="volume_storage"
        help="Storage with the images content type for project volumes. Leave blank to use the same storage as server disks."
      >
        <UInput
          v-model="form.volume_storage"
          name="volume_storage"
          placeholder="Same as server disks"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
      <UFormField
        label="Snapshot storage"
        name="snapshot_storage"
        help="Storage with the images content type for snapshots. Leave blank to use the same storage as server disks."
      >
        <UInput
          v-model="form.snapshot_storage"
          name="snapshot_storage"
          placeholder="Same as server disks"
          autocomplete="off"
          class="w-full"
        />
      </UFormField>
    </fieldset>

    <fieldset v-if="editing" class="flex flex-col gap-5">
      <legend class="mb-3 font-bold tracking-tight text-highlighted">Backups</legend>
      <UFormField
        label="Backup storage"
        name="backup_storage"
        help="A Proxmox Backup Server storage. Leave blank to turn backups off here. Existing backups stay restorable."
        :error="errors.backup_storage"
      >
        <UInput
          v-model="form.backup_storage"
          name="backup_storage"
          list="backup-storages"
          placeholder="None"
          autocomplete="off"
          class="w-full"
        />
        <datalist id="backup-storages">
          <option v-for="s in pbsOptions" :key="s" :value="s" />
        </datalist>
      </UFormField>
      <UFormField
        label="Backup window (UTC)"
        name="backup_window"
        help="Scheduled backups start inside this window. It may cross midnight."
        :error="errors.backup_window"
      >
        <div class="flex items-center gap-2">
          <UInput v-model="form.window_start" name="backup_window_start" type="time" />
          <span class="text-muted">to</span>
          <UInput v-model="form.window_end" name="backup_window_end" type="time" />
        </div>
      </UFormField>
    </fieldset>

    <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
      <UButton color="neutral" variant="ghost" class="justify-center" @click="emit('cancel')">
        Cancel
      </UButton>
      <UButton type="submit" :loading="saveLocation.isPending.value" class="justify-center">
        {{ editing ? 'Save changes' : 'Connect location' }}
      </UButton>
    </div>
  </form>
</template>
