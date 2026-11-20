import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import type { Ref } from 'vue'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { setMe, useSession } from '@/lib/session'

export type AdminUser = components['schemas']['AdminUser']
export type AdminUserInput = components['schemas']['AdminUserInput']
export const PER_PAGE = 50

const key = ['admin', 'users']

export function useAdminUsers(page: Ref<number>) {
  return useQuery({
    queryKey: [...key, page],
    queryFn: async () =>
      unwrap(
        await api.GET('/admin/users', {
          params: { query: { page: page.value, per_page: PER_PAGE } },
        }),
      ),
  })
}

export function useCreateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: {
      name: string
      email: string
      password: string
      is_admin: boolean
    }) => unwrap(await api.POST('/admin/users', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useUpdateUser() {
  const qc = useQueryClient()
  const { me } = useSession()
  return useMutation({
    mutationFn: async (v: { id: number; body: AdminUserInput }): Promise<AdminUser> =>
      unwrap(await api.PUT('/admin/users/{id}', { params: { path: { id: v.id } }, body: v.body })),
    onSuccess: async (u) => {
      if (u.id === me.value?.id) {
        setMe({
          id: u.id,
          name: u.name,
          email: u.email,
          is_admin: u.is_admin,
          totp_enabled: u.totp_enabled,
        })
      }
      await qc.invalidateQueries({ queryKey: key })
    },
  })
}

export function useResetPassword() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id: number; password: string; remove_passkeys: boolean }) =>
      unwrap(
        await api.POST('/admin/users/{id}/password', {
          params: { path: { id: v.id } },
          body: { password: v.password, remove_passkeys: v.remove_passkeys },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useDisableTotp() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/users/{id}/totp', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useRemovePasskeys() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/users/{id}/passkeys', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id: number; takeOver: boolean }) =>
      unwrap(
        await api.DELETE('/admin/users/{id}', {
          params: { path: { id: v.id }, query: v.takeOver ? { take_over: true } : {} },
        }),
      ),
    onSuccess: async (_d, v) => {
      await qc.invalidateQueries({ queryKey: key })
      if (v.takeOver) await qc.invalidateQueries({ queryKey: ['admin', 'projects'] })
    },
  })
}
