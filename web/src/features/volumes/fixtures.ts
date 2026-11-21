import type { Location, Volume } from '@/api/types'

export function makeVolume(over: Partial<Volume> = {}): Volume {
  return {
    id: 1,
    name: 'data',
    labels: {},
    size: 20,
    location: { id: 1, name: 'home', description: '' },
    server: null,
    status: 'available',
    linux_device: '/dev/disk/by-id/virtio-nb-vol-1',
    created: '2026-11-18T10:00:00Z',
    ...over,
  }
}

export function makeLocation(over: Partial<Location> = {}): Location {
  return {
    id: 1,
    name: 'home',
    description: '',
    networks: { ipv4: true, ipv6: false },
    volumes_available: true,
    ...over,
  }
}
