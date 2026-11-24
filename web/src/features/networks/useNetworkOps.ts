// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import type { NetworkGuestConfig } from '@/api/types'
import { useActionPoll } from '@/lib/useActionPoll'
import { useRefreshNetworks, type NetworkActionResult } from './queries'

// Tracks an attach or detach action and the guest commands that come with it.
export function useNetworkOps() {
  const qc = useQueryClient()
  const refresh = useRefreshNetworks()
  const actionId = ref<number | null>(null)
  const { action, running, error } = useActionPoll(actionId, { onDone: () => void refresh() })
  const guest = ref<{ config: NetworkGuestConfig; serverName: string } | null>(null)
  const showing = ref(false)

  function started(res: NetworkActionResult, serverName: string) {
    qc.setQueryData(['action', res.action.id], res.action)
    void qc.invalidateQueries({ queryKey: ['action', res.action.id], refetchType: 'none' })
    actionId.value = res.action.id
    void refresh()
    if (res.guest_config) {
      guest.value = { config: res.guest_config, serverName }
      showing.value = true
    }
  }

  return { action, running, error, guest, showing, started }
}
