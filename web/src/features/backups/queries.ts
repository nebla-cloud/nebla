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

import { computed, type Ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import type { Action, Server } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

export type Backup = components['schemas']['Backup']
export type BackupSettings = components['schemas']['BackupSettings']

const POLL_MS = 3000
export const MIN_KEEP = 1
export const MAX_KEEP = 14
export const DEFAULT_KEEP = 7

// Statuses that settle on their own, so the list keeps polling.
const moving: Backup['status'][] = ['queued', 'creating', 'restoring', 'deleting']
export const isMoving = (b: Backup) => moving.includes(b.status)
export const isPending = (b: Backup) => b.status === 'queued' || b.status === 'creating'

export const backupsKey = (project: number, server: number) => ['backups', project, server]

export function useBackups(serverId: Ref<number>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: backupsKey(projectId.value, serverId.value),
      queryFn: async (): Promise<Backup[]> =>
        unwrap(
          await api.GET('/servers/{id}/backups', { params: { path: { id: serverId.value } } }),
        ).backups,
      enabled: projectId.value > 0 && serverId.value > 0,
      refetchInterval: (q: { state: { data?: Backup[] } }) =>
        q.state.data?.some(isMoving) ? POLL_MS : false,
    })),
  )
}

// Refreshes everything a backup change can touch.
export function useRefreshBackups() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['backups', 'server', 'servers', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function saveBackupSettings(id: number, backups: BackupSettings): Promise<Server> {
  return unwrap(await api.PUT('/servers/{id}', { ...path(id), body: { backups } }))
}

export async function backUpNow(id: number): Promise<Backup> {
  return unwrap(await api.POST('/servers/{id}/backups', path(id)))
}

export async function deleteBackup(id: number, backup: number): Promise<Action> {
  return unwrap(
    await api.DELETE('/servers/{id}/backups/{backup}', {
      params: { path: { id, backup } },
    }),
  ).action
}

export async function restoreBackup(id: number, backup: number): Promise<Action> {
  return unwrap(
    await api.POST('/servers/{id}/actions/restore_backup', { ...path(id), body: { backup } }),
  ).action
}
