import type { Server } from '@/api/types'

export function makeServer(over: Partial<Server> = {}): Server {
  return {
    id: 1,
    name: 'web1',
    status: 'running',
    location: { id: 1, name: 'home', description: '' },
    server_type: {
      id: 1,
      name: 'small-2',
      description: '',
      cores: 2,
      memory_mb: 4096,
      disk_gb: 40,
      locations: [1],
      deprecated: false,
    },
    image: {
      id: 1,
      name: 'Debian 13',
      description: '',
      os_flavor: 'debian',
      os_version: '13',
      locations: [1],
    },
    public_net: { ipv4: { ip: '203.0.113.10' }, ipv6: null },
    labels: {},
    disk_gb: 40,
    created: '2026-11-18T10:00:00Z',
    ...over,
  }
}
