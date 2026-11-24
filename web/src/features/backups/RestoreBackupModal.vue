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
import ConfirmByName from '@/components/ConfirmByName.vue'
import { formatDateTime } from '@/lib/format'
import type { Backup } from './queries'

const props = defineProps<{
  name: string
  backup: Backup | null
  loading?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ confirm: [] }>()
const open = defineModel<boolean>('open', { default: false })

const when = computed(() =>
  props.backup ? formatDateTime(props.backup.completed ?? props.backup.created) : '',
)
</script>

<template>
  <ConfirmByName
    v-model:open="open"
    :name="name"
    title="Restore this server"
    confirm-label="Restore server"
    :loading="loading"
    :error="error"
    @confirm="emit('confirm')"
  >
    <template #intro>
      <p class="text-sm text-default">
        Restore {{ name }} to {{ when }}? The current disk will be replaced. Anything written since
        then is lost.
      </p>
      <p v-if="backup?.restore_resets_identity" class="text-sm text-default">
        This server's network changed since this backup. Its network will be reconfigured and SSH
        host keys will change.
      </p>
    </template>
  </ConfirmByName>
</template>
