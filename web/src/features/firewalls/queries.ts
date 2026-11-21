import { computed, type Ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import type { Action } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

export type Firewall = components['schemas']['Firewall']
export type FirewallRule = components['schemas']['FirewallRule']
export type FirewallResource = components['schemas']['FirewallResource']

export function useFirewalls() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['firewalls', projectId.value]),
    queryFn: async (): Promise<Firewall[]> =>
      unwrap(await api.GET('/firewalls', { params: { query: { per_page: 50 } } })).firewalls,
    enabled: computed(() => projectId.value > 0),
  })
}

export function useFirewall(id: Ref<number>) {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['firewall', projectId.value, id.value]),
    queryFn: async (): Promise<Firewall> =>
      unwrap(await api.GET('/firewalls/{id}', { params: { path: { id: id.value } } })),
    enabled: computed(() => projectId.value > 0 && id.value > 0),
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['firewalls', 'firewall', 'server', 'servers'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

export function useCreateFirewall() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (body: { name: string; labels: Record<string, string> }) =>
      unwrap(await api.POST('/firewalls', { body })),
    onSuccess: invalidate,
  })
}

export function useRenameFirewall() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (v: { id: number; name: string }): Promise<Firewall> =>
      unwrap(
        await api.PUT('/firewalls/{id}', { params: { path: { id: v.id } }, body: { name: v.name } }),
      ),
    onSuccess: invalidate,
  })
}

export function useDeleteFirewall() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (id: number): Promise<Action[]> =>
      unwrap(await api.DELETE('/firewalls/{id}', { params: { path: { id } } })).actions,
    onSuccess: invalidate,
  })
}

export function useSetRules() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (v: { id: number; rules: FirewallRule[] }): Promise<Action[]> =>
      unwrap(
        await api.POST('/firewalls/{id}/actions/set_rules', {
          params: { path: { id: v.id } },
          body: { rules: v.rules },
        }),
      ).actions,
    onSuccess: invalidate,
  })
}

export function useApplyTo() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (v: { id: number; resources: FirewallResource[] }): Promise<Action[]> =>
      unwrap(
        await api.POST('/firewalls/{id}/actions/apply_to_resources', {
          params: { path: { id: v.id } },
          body: { apply_to: v.resources },
        }),
      ).actions,
    onSuccess: invalidate,
  })
}

export function useRemoveFrom() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async (v: { id: number; resources: FirewallResource[] }): Promise<Action[]> =>
      unwrap(
        await api.POST('/firewalls/{id}/actions/remove_from_resources', {
          params: { path: { id: v.id } },
          body: { remove_from: v.resources },
        }),
      ).actions,
    onSuccess: invalidate,
  })
}

export function ruleCounts(f: Firewall): { inbound: number; outbound: number } {
  const inbound = f.rules.filter((r) => r.direction === 'in').length
  return { inbound, outbound: f.rules.length - inbound }
}
