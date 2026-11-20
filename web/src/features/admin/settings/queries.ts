import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { useSession } from '@/lib/session'

export type AdminSettings = components['schemas']['AdminSettings']

const key = ['admin', 'settings']

export function useAdminSettings() {
  return useQuery({
    queryKey: key,
    queryFn: async (): Promise<AdminSettings> => unwrap(await api.GET('/admin/settings')),
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  const session = useSession()
  return useMutation({
    mutationFn: async (body: AdminSettings): Promise<AdminSettings> =>
      unwrap(await api.PUT('/admin/settings', { body })),
    onSuccess: async (data) => {
      qc.setQueryData(key, data)
      // The login page reads signup_enabled from the manifest.
      await session.refresh()
    },
  })
}
